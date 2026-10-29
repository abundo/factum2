package web

import (
	"errors"
	"log/slog"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func (ctrl *Controller) ipamNamespaceName(id uint) (string, error) {
	if id == 0 {
		return "", nil
	}
	var ns models.IpamNamespace
	if err := ctrl.DB.First(&ns, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", &ipam.StatusError{Status: 404, Message: "namespace not found"}
		}
		return "", err
	}
	return ns.Name, nil
}

func ipamMirrorsNetbox(namespaceName string, netboxID uint, source string) bool {
	return namespaceName == netbox.NetboxNamespaceName || netboxID != 0 || source == models.VRFSourceNetbox
}

func (ctrl *Controller) requireNetboxClient() (*netboxtool.NetboxClient, error) {
	nb, err := ctrl.netboxClientIfConfigured()
	if err != nil {
		return nil, err
	}
	if nb == nil {
		return nil, &ipam.StatusError{
			Status:  400,
			Message: "NetBox is not configured; VRFs and prefixes in the Netbox namespace cannot be changed here",
		}
	}
	return nb, nil
}

func netboxUpstreamErr(err error) error {
	return &ipam.StatusError{Status: 502, Message: "NetBox: " + err.Error()}
}

func (ctrl *Controller) publishNewVRF(row *models.IpamVRF) error {
	if row == nil || row.IsDefault {
		return nil
	}
	return ctrl.writeVRFToNetbox(row)
}

func (ctrl *Controller) publishVRFUpdate(before, row *models.IpamVRF) error {
	if row == nil || row.IsDefault {
		return nil
	}
	if row.NetboxID != 0 && before != nil &&
		before.Name == row.Name && before.Description == row.Description &&
		before.RD == row.RD && before.ImportRT == row.ImportRT && before.ExportRT == row.ExportRT {
		return nil
	}
	return ctrl.writeVRFToNetbox(row)
}

func (ctrl *Controller) writeVRFToNetbox(row *models.IpamVRF) error {
	name, err := ctrl.ipamNamespaceName(row.NamespaceID)
	if err != nil {
		return err
	}
	if !ipamMirrorsNetbox(name, row.NetboxID, row.Source) {
		return nil
	}
	nb, err := ctrl.requireNetboxClient()
	if err != nil {
		return err
	}
	importIDs, err := nb.EnsureRouteTargetIDs(netbox.SplitRouteTargets(row.ImportRT))
	if err != nil {
		return netboxUpstreamErr(err)
	}
	exportIDs, err := nb.EnsureRouteTargetIDs(netbox.SplitRouteTargets(row.ExportRT))
	if err != nil {
		return netboxUpstreamErr(err)
	}
	var rd any
	if row.RD != "" {
		rd = row.RD
	}
	if row.NetboxID != 0 {
		err = nb.UpdateVRF(row.NetboxID, map[string]any{
			"name":           row.Name,
			"description":    row.Description,
			"rd":             rd,
			"import_targets": importIDs,
			"export_targets": exportIDs,
		})
		if err != nil {
			return netboxUpstreamErr(err)
		}
		return nil
	}
	id, err := nb.CreateVRFWithTargets(row.Name, row.RD, row.Description, importIDs, exportIDs)
	if err != nil {
		return netboxUpstreamErr(err)
	}
	if err := ctrl.stampVRF(row, id); err != nil {
		if delErr := nb.DeleteVRF(id); delErr != nil {
			slog.Error("netbox VRF rollback failed", "netbox_id", id, "err", delErr)
		}
		return err
	}
	return nil
}

func (ctrl *Controller) stampVRF(row *models.IpamVRF, netboxID uint) error {
	err := ctrl.DB.Model(&models.IpamVRF{}).Where("id = ?", row.ID).Updates(map[string]any{
		"source":    models.VRFSourceNetbox,
		"netbox_id": netboxID,
	}).Error
	if err != nil {
		return err
	}
	row.Source = models.VRFSourceNetbox
	row.NetboxID = netboxID
	return nil
}

func (ctrl *Controller) publishNewPrefix(row *models.IpamPrefix) error {
	if row == nil {
		return nil
	}
	return ctrl.writePrefixToNetbox(row)
}

func (ctrl *Controller) publishPrefixUpdate(before, row *models.IpamPrefix) error {
	if row == nil {
		return nil
	}
	if row.NetboxID != 0 && before != nil && before.Description == row.Description {
		return nil
	}
	return ctrl.writePrefixToNetbox(row)
}

func (ctrl *Controller) writePrefixToNetbox(row *models.IpamPrefix) error {
	name, err := ctrl.ipamNamespaceName(row.NamespaceID)
	if err != nil {
		return err
	}
	if !ipamMirrorsNetbox(name, row.NetboxID, row.Source) {
		return nil
	}
	nb, err := ctrl.requireNetboxClient()
	if err != nil {
		return err
	}
	if row.NetboxID != 0 {
		if err := nb.UpdatePrefix(row.NetboxID, map[string]any{"description": row.Description}); err != nil {
			return netboxUpstreamErr(err)
		}
		return nil
	}
	vrfNetboxID, err := ctrl.ensurePrefixVRF(nb, row.VRFID)
	if err != nil {
		return err
	}
	id, err := nb.CreatePrefixInVRF(row.Prefix, row.Description, vrfNetboxID)
	if err != nil {
		return netboxUpstreamErr(err)
	}
	if err := ctrl.stampPrefix(row, id); err != nil {
		if delErr := nb.DeletePrefix(id); delErr != nil {
			slog.Error("netbox prefix rollback failed", "netbox_id", id, "err", delErr)
		}
		return err
	}
	return nil
}

func (ctrl *Controller) ensurePrefixVRF(nb *netboxtool.NetboxClient, vrfID uint) (uint, error) {
	if vrfID == 0 {
		return 0, nil
	}
	var vrf models.IpamVRF
	if err := ctrl.DB.First(&vrf, vrfID).Error; err != nil {
		return 0, err
	}
	if vrf.IsDefault {
		return 0, nil
	}
	if vrf.NetboxID != 0 {
		return vrf.NetboxID, nil
	}
	if err := ctrl.writeVRFToNetbox(&vrf); err != nil {
		return 0, err
	}
	return vrf.NetboxID, nil
}

func (ctrl *Controller) stampPrefix(row *models.IpamPrefix, netboxID uint) error {
	err := ctrl.DB.Model(&models.IpamPrefix{}).Where("id = ?", row.ID).Updates(map[string]any{
		"source":    models.VRFSourceNetbox,
		"netbox_id": netboxID,
	}).Error
	if err != nil {
		return err
	}
	row.Source = models.VRFSourceNetbox
	row.NetboxID = netboxID
	return nil
}
