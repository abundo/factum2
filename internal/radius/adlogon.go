package radius

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/ldapauth"
	"github.com/oiweiwei/go-msrpc/dcerpc"
	"github.com/oiweiwei/go-msrpc/msrpc/dtyp"
	"github.com/oiweiwei/go-msrpc/msrpc/epm/epm/v3"
	"github.com/oiweiwei/go-msrpc/msrpc/nrpc/logon/v1"
	"github.com/oiweiwei/go-msrpc/ssp"
	"github.com/oiweiwei/go-msrpc/ssp/credential"
	"github.com/oiweiwei/go-msrpc/ssp/gssapi"
)

// ErrBadCredentials is a wrong username or password. Callers must not
// treat it as a directory outage.
var ErrBadCredentials = errors.New("bad credentials")

// Machine is the Active Directory computer account that opens the
// NETLOGON channel used to check MS-CHAPv2. Account is the SAM name,
// including the trailing $.
type Machine struct {
	Account  string `json:"account"`
	Password string `json:"password"`
	Domain   string `json:"domain"`
}

// verifyMSCHAP asks a domain controller whether the MS-CHAPv2 response
// is valid. The returned 16 bytes are MD4 of the NT hash, which is what
// the success packet's authenticator response is built from.
func verifyMSCHAP(ctx context.Context, cfg ldapauth.Config, machine Machine, username string, challenge, ntResponse []byte) ([]byte, error) {
	if !strings.EqualFold(cfg.ServerType, "ad") {
		return nil, errMSCHAPNotAD
	}
	if strings.TrimSpace(machine.Account) == "" || machine.Password == "" {
		return nil, errMSCHAPConfig
	}
	var last error
	for _, host := range []string{cfg.Host, cfg.Host2} {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		key, err := verifyMSCHAPHost(ctx, host, machine, cfg.BaseDN, username, challenge, ntResponse)
		if err == nil || errors.Is(err, ErrBadCredentials) {
			return key, err
		}
		last = err
	}
	if last == nil {
		last = errors.New("no directory host configured")
	}
	return nil, last
}

func verifyMSCHAPHost(ctx context.Context, host string, machine Machine, baseDN, username string, challenge, ntResponse []byte) ([]byte, error) {
	account := machine.Account
	if !strings.HasSuffix(account, "$") {
		account += "$"
	}
	workstation := strings.TrimSuffix(account, "$")
	domain := strings.TrimSpace(machine.Domain)
	if domain == "" {
		domain = dnsDomain(baseDN)
	}
	cred := credential.NewFromPassword(account, machine.Password,
		credential.Workstation(workstation), credential.Domain(domain))
	ctx = gssapi.NewSecurityContext(ctx,
		gssapi.WithCredential(cred),
		gssapi.WithMechanismFactory(ssp.Netlogon),
	)
	cc, err := dcerpc.Dial(ctx, host, epm.EndpointMapper(ctx, host))
	if err != nil {
		return nil, fmt.Errorf("netlogon dial %s: %w", host, err)
	}
	defer cc.Close(ctx)

	cli, err := logon.NewSecureChannelClient(ctx, cc, dcerpc.WithSeal(), dcerpc.WithEndpoint("ncacn_ip_tcp:"))
	if err != nil {
		return nil, fmt.Errorf("netlogon channel %s: %w", host, err)
	}
	dc := cli.DomainControllerInfo()
	server := ""
	if dc != nil {
		server = dc.DomainControllerName
	}
	resp, err := cli.SAMLogonEx(ctx, &logon.SAMLogonExRequest{
		LogonServer:  server,
		ComputerName: workstation,
		LogonLevel:   logon.LogonInfoClassNetworkTransitiveInformation,
		LogonInformation: &logon.Level{
			Value: &logon.Level_LogonNetworkTransitive{LogonNetworkTransitive: &logon.NetworkInfo{
				Identity: &logon.LogonIdentityInfo{
					LogonDomainName: &dtyp.UnicodeString{Buffer: domain},
					UserName:        &dtyp.UnicodeString{Buffer: username},
					Workstation:     &dtyp.UnicodeString{Buffer: workstation},
				},
				LMChallenge:         &logon.LMChallenge{Data: challenge},
				NTChallengeResponse: &logon.String{Buffer: ntResponse},
				LMChallengeResponse: &logon.String{Buffer: make([]byte, 24)},
			}},
		},
		ValidationLevel: logon.ValidationInfoClassSAMInfo4,
	})
	if err != nil {
		if badLogon(err) {
			return nil, ErrBadCredentials
		}
		return nil, fmt.Errorf("netlogon %s: %w", host, err)
	}
	if resp == nil || badLogonCode(resp.Return) {
		return nil, ErrBadCredentials
	}
	if resp.Return != 0 {
		return nil, fmt.Errorf("netlogon %s: status %08x", host, uint32(resp.Return))
	}
	info, ok := resp.ValidationInformation.GetValue().(*logon.ValidationSAMInfo4)
	if !ok || info == nil || info.UserSessionKey == nil || len(info.UserSessionKey.Data) < 2 {
		return nil, fmt.Errorf("netlogon %s: no session key", host)
	}
	key := append(append([]byte{}, info.UserSessionKey.Data[0].Data...), info.UserSessionKey.Data[1].Data...)
	if len(key) != 16 || allZero(key) {
		return nil, fmt.Errorf("netlogon %s: empty session key", host)
	}
	return key, nil
}

func dnsDomain(base string) string {
	var parts []string
	for _, p := range strings.Split(base, ",") {
		p = strings.TrimSpace(p)
		if len(p) > 3 && strings.EqualFold(p[:3], "dc=") {
			parts = append(parts, p[3:])
		}
	}
	return strings.Join(parts, ".")
}

func badLogon(err error) bool {
	s := strings.ToLower(err.Error())
	for _, mark := range []string{
		"status_wrong_password",
		"status_logon_failure",
		"status_no_such_user",
		"status_account_restriction",
		"status_account_disabled",
		"status_account_locked_out",
		"status_password_expired",
		"status_password_must_change",
		"status_invalid_logon_hours",
		"c000006a", "c000006d", "c0000064",
	} {
		if strings.Contains(s, mark) {
			return true
		}
	}
	return false
}

func badLogonCode(code int32) bool {
	switch uint32(code) {
	case 0xC000006A, 0xC000006D, 0xC0000064, 0xC000006E,
		0xC0000071, 0xC0000072, 0xC0000234, 0xC000006F, 0xC0000224:
		return true
	default:
		return false
	}
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

var (
	errMSCHAPNotAD  = errors.New("MS-CHAPv2 requires Active Directory")
	errMSCHAPConfig = errors.New("MS-CHAPv2 is not configured")
)
