# Copy the legacy NetBox custom fields "librenms_id" and "librenms_device_id"
# into the "destination" field as a librenms:<id> pair. Other systems already
# stored there (for example zabbix:7) are kept. The legacy values are left
# in place. Delete librenms_id and librenms_device_id in the NetBox UI after
# this script has run and you have checked a few devices.
#
# Different installations used one name or the other. When one device has
# both and they disagree, the row is skipped. A destination value that
# already has a librenms id is left alone.
#
# Create the destination field first:
#
#   factum2-netbox check --update
#
# Then pipe this file into NetBox's nbshell. Django executes stdin as code
# and then exits:
#
#   cd /opt/netbox
#   ./venv/bin/python netbox/manage.py nbshell < netbox_migrate_destination_field.py
#
# From a host, with the script on the host and NetBox in a container:
#
#   docker compose exec -T netbox \
#     /opt/netbox/venv/bin/python /opt/netbox/netbox/manage.py nbshell \
#     < netbox_migrate_destination_field.py
#
# Safe to run more than once. Rows are updated with a SQL UPDATE, so
# NetBox webhooks and the changelog do not see each rewrite.
#
# The written form matches factum's reader (internal/netboxtool/destination.go):
# space-separated system:id pairs, or a JSON object when reading.

import json

from django.db import transaction
from django.db.models import Q


def _cf_text(value):
    """Text form of a custom-field scalar. Empty when there is nothing to copy."""
    if value is None or isinstance(value, bool):
        return ""
    if isinstance(value, int):
        if value == 0:
            return ""
        return str(value)
    if isinstance(value, float):
        if value != value or value == 0:  # NaN or zero
            return ""
        if value.is_integer():
            return str(int(value))
        return str(value).strip()
    if isinstance(value, str):
        return value.strip()
    return ""


def _system_ok(system):
    if system == "":
        return False
    for ch in system:
        if ("a" <= ch <= "z") or ("0" <= ch <= "9") or ch in "_-":
            continue
        return False
    return True


def _id_text(value):
    """Id string, '' when empty, or None when the value must not be dropped."""
    if isinstance(value, (dict, list, bool)):
        return None
    if isinstance(value, float) and value == value and not value.is_integer():
        return None
    text = _cf_text(value)
    if text == "" or text == "0":
        return ""
    if any(ch.isspace() for ch in text):
        return None
    return text


def _remember(out, system, ident):
    system = system.strip().lower()
    if not _system_ok(system) or ident is None or ident == "" or ident == "0" or any(ch.isspace() for ch in ident):
        return False
    prev = out.get(system)
    if prev is not None and prev != ident:
        return False
    out[system] = ident
    return True


def _ids_from_mapping(obj):
    out = {}
    for key, raw in obj.items():
        ident = _id_text(raw)
        if ident is None:
            return None
        if ident == "":
            continue
        if not _remember(out, str(key), ident):
            return None
    return out


def _ids_from_tokens(text):
    out = {}
    parts = text.split()
    i = 0
    while i < len(parts):
        tok = parts[i]
        if tok.endswith(":") and tok != ":":
            system = tok[:-1]
            i += 1
            if i >= len(parts):
                return None
            ident = parts[i]
            i += 1
        elif ":" in tok:
            system, ident = tok.split(":", 1)
            i += 1
        else:
            return None
        if not _remember(out, system, ident.strip()):
            return None
    return out


def _parse_destination(value):
    """Return the system->id map, or None when the value is unreadable."""
    if value is None or value == "":
        return {}
    if isinstance(value, dict):
        return _ids_from_mapping(value)
    if isinstance(value, str):
        text = value.strip()
        if text == "":
            return {}
        if text.startswith("{"):
            try:
                parsed = json.loads(text)
            except ValueError:
                return None
            if not isinstance(parsed, dict):
                return None
            return _ids_from_mapping(parsed)
        return _ids_from_tokens(text)
    return None


def _format_destination(ids):
    if not ids:
        return ""
    return " ".join("%s:%s" % (key, ids[key]) for key in sorted(ids))


def _legacy_librenms(data):
    """Return (id, problem). problem is 'conflict' when the two old fields disagree."""
    a = _cf_text(data.get("librenms_id"))
    b = _cf_text(data.get("librenms_device_id"))
    if a == "0":
        a = ""
    if b == "0":
        b = ""
    if a and b and a != b:
        return "", "conflict"
    return a or b, ""


def _merge_librenms(current, librenms_id):
    """Return (new_text_or_None, status).

    status is updated, unchanged, kept, or unreadable. kept means destination
    already has a different librenms id and this row was not rewritten.
    """
    ids = _parse_destination(current)
    if ids is None:
        return None, "unreadable"
    existing = ids.get("librenms", "")
    if not librenms_id or existing == librenms_id:
        return None, "unchanged"
    if existing:
        return None, "kept"
    ids["librenms"] = librenms_id
    return _format_destination(ids), "updated"


def _assigned_models(custom_field):
    """Models this custom field is assigned to (NetBox 3 content_types or 4 object_types)."""
    relation = getattr(custom_field, "object_types", None)
    if relation is None:
        relation = custom_field.content_types
    models = []
    seen = set()
    for content_type in relation.all():
        model = content_type.model_class()
        if model is None:
            continue
        key = (model._meta.app_label, model._meta.model_name)
        if key in seen:
            continue
        seen.add(key)
        field_names = {field.name for field in model._meta.concrete_fields}
        if "custom_field_data" not in field_names:
            print("skip %s.%s: no custom_field_data column" % key)
            continue
        models.append(model)
    return models


def migrate_destination_field():
    from extras.models import CustomField

    fields = {}
    for name in ("destination", "librenms_id", "librenms_device_id"):
        try:
            fields[name] = CustomField.objects.get(name=name)
        except CustomField.DoesNotExist:
            fields[name] = None
    if fields["destination"] is None:
        print("custom field destination is not defined; create it with factum2-netbox check --update, then re-run")
        return
    if fields["librenms_id"] is None and fields["librenms_device_id"] is None:
        print("neither librenms_id nor librenms_device_id is defined; nothing to copy")
        return

    models = {}
    for custom_field in fields.values():
        if custom_field is None:
            continue
        for model in _assigned_models(custom_field):
            models[(model._meta.app_label, model._meta.model_name)] = model

    destination_models = set()
    if fields["destination"] is not None:
        for model in _assigned_models(fields["destination"]):
            destination_models.add((model._meta.app_label, model._meta.model_name))

    counts = {"updated": 0, "unchanged": 0, "kept": 0, "conflict": 0, "unreadable": 0}
    with transaction.atomic():
        for (app_label, model_name), model in sorted(models.items()):
            label = "%s.%s" % (app_label, model_name)
            if (app_label, model_name) not in destination_models:
                print("note: %s is not assigned the destination field; the value is stored anyway" % label)
            model_counts = {"updated": 0, "unchanged": 0, "kept": 0, "conflict": 0, "unreadable": 0}
            rows = model.objects.filter(
                Q(custom_field_data__has_key="librenms_id") | Q(custom_field_data__has_key="librenms_device_id")
            ).iterator()
            for obj in rows:
                data = dict(obj.custom_field_data or {})
                ident, problem = _legacy_librenms(data)
                if problem == "conflict":
                    print("%s pk=%s: librenms_id and librenms_device_id disagree; left unchanged" % (label, obj.pk))
                    model_counts["conflict"] += 1
                    continue
                new_value, status = _merge_librenms(data.get("destination"), ident)
                if status == "updated":
                    data["destination"] = new_value
                    model.objects.filter(pk=obj.pk).update(custom_field_data=data)
                elif status == "kept":
                    print("%s pk=%s: destination already has librenms id %r, legacy value is %r; left unchanged" % (
                        label, obj.pk, _parse_destination(data.get("destination")).get("librenms"), ident))
                elif status == "unreadable":
                    print("%s pk=%s: destination value is not readable; left unchanged" % (label, obj.pk))
                model_counts[status] += 1
            for key, n in model_counts.items():
                counts[key] += n
            print("%s: updated %d, left unchanged %d, kept existing librenms %d, conflicts %d, unreadable %d" % (
                label,
                model_counts["updated"],
                model_counts["unchanged"],
                model_counts["kept"],
                model_counts["conflict"],
                model_counts["unreadable"],
            ))

    print("done: updated %d, left unchanged %d, kept existing librenms %d, conflicts %d, unreadable %d" % (
        counts["updated"], counts["unchanged"], counts["kept"], counts["conflict"], counts["unreadable"]))
    print("librenms_id and librenms_device_id were not removed. Delete those custom fields in the NetBox UI when you are satisfied with destination.")


migrate_destination_field()
