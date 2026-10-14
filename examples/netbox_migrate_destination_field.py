#!/usr/bin/env python3
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
# NetBox URL and token are read from the factum settings row. The database
# connection comes from factum2.yaml (default /etc/factum2/factum2.yaml).
# NETBOX_URL / NETBOX_TOKEN, or --url / --token, override that row.
#
#   python3 examples/netbox_migrate_destination_field.py
#   python3 examples/netbox_migrate_destination_field.py --dry-run
#
# Safe to run more than once. Each write is a PATCH of
# custom_fields.destination only. The value is the full list of system:id
# pairs, so other systems already stored there are kept. A PATCH does fire
# the object_updated webhook.
#
# The written form matches factum's reader (internal/netboxtool/destination.go):
# space-separated system:id pairs, or a JSON object when reading.

import argparse
import json
import sys

from netbox_migrate_common import (
    APIError,
    OBJECT_PATHS,
    add_common_args,
    content_type_resolver,
    find_field,
    iter_filtered,
    object_types,
    open_client,
    patch_custom_field,
    row_label,
)

DEST_FIELD = "destination"
LEGACY_FIELDS = ("librenms_id", "librenms_device_id")


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


def parse_destination(value):
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


def format_destination(ids):
    if not ids:
        return ""
    return " ".join("%s:%s" % (key, ids[key]) for key in sorted(ids))


def legacy_librenms(data):
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


def merge_librenms(current, librenms_id):
    """Return (new_text_or_None, status).

    status is updated, unchanged, kept, or unreadable. kept means destination
    already has a different librenms id and this row was not rewritten.
    """
    ids = parse_destination(current)
    if ids is None:
        return None, "unreadable"
    existing = ids.get("librenms", "")
    if not librenms_id or existing == librenms_id:
        return None, "unchanged"
    if existing:
        return None, "kept"
    ids["librenms"] = librenms_id
    return format_destination(ids), "updated"


def migrate(nb, dry_run):
    destination = find_field(nb, DEST_FIELD)
    if destination is None:
        print("custom field destination is not defined; create it with factum2-netbox check --update, then re-run")
        return 1
    if destination.get("type") not in (None, "", "text"):
        print("custom field destination has type %r, want text" % (destination.get("type"),))
        return 1
    resolve = content_type_resolver(nb)
    dest_types = set(object_types(destination, resolve))
    legacy_fields = []
    types = []
    for name in LEGACY_FIELDS:
        field = find_field(nb, name)
        if field is None:
            continue
        legacy_fields.append(name)
        for obj_type in object_types(field, resolve):
            if obj_type not in types:
                types.append(obj_type)
    if not legacy_fields:
        print("neither librenms_id nor librenms_device_id is defined; nothing to copy")
        return 0

    counts = {"updated": 0, "unchanged": 0, "kept": 0, "conflict": 0, "unreadable": 0}
    failed = False
    for obj_type in types:
        path = OBJECT_PATHS.get(obj_type)
        if path is None:
            print("skip %s: no REST path for this object type" % obj_type)
            failed = True
            continue
        if obj_type not in dest_types:
            print("%s is not assigned the destination field; run factum2-netbox check --update" % obj_type)
            failed = True
            continue
        model_counts = {key: 0 for key in counts}
        seen = set()
        rows = []
        for name in legacy_fields:
            for row in iter_filtered(nb, path, name, "gt"):
                rid = row.get("id")
                if rid in seen:
                    continue
                seen.add(rid)
                rows.append(row)
        for row in rows:
            fields = row.get("custom_fields") or {}
            ident, problem = legacy_librenms(fields)
            label = row_label(row)
            if problem == "conflict":
                print("%s %s: librenms_id and librenms_device_id disagree; left unchanged" % (obj_type, label))
                model_counts["conflict"] += 1
                continue
            if ident == "":
                model_counts["unchanged"] += 1
                continue
            new_value, status = merge_librenms(fields.get(DEST_FIELD), ident)
            if status == "updated":
                if dry_run:
                    print("dry-run: would set %s %s destination=%s" % (obj_type, label, new_value))
                else:
                    patch_custom_field(nb, path, row, DEST_FIELD, new_value)
                    print("updated %s %s destination=%s" % (obj_type, label, new_value))
            elif status == "kept":
                current = parse_destination(fields.get(DEST_FIELD)) or {}
                print("%s %s: destination already has librenms id %r, legacy value is %r; left unchanged" % (
                    obj_type, label, current.get("librenms"), ident))
            elif status == "unreadable":
                print("%s %s: destination value is not readable; left unchanged" % (obj_type, label))
            model_counts[status] += 1
        for key, n in model_counts.items():
            counts[key] += n
        print("%s: updated %d, left unchanged %d, kept existing librenms %d, conflicts %d, unreadable %d" % (
            obj_type,
            model_counts["updated"],
            model_counts["unchanged"],
            model_counts["kept"],
            model_counts["conflict"],
            model_counts["unreadable"],
        ))

    print("done: updated %d, left unchanged %d, kept existing librenms %d, conflicts %d, unreadable %d" % (
        counts["updated"], counts["unchanged"], counts["kept"], counts["conflict"], counts["unreadable"]))
    print("librenms_id and librenms_device_id were not removed. Delete those custom fields in the NetBox UI when you are satisfied with destination.")
    if failed or counts["conflict"] or counts["unreadable"]:
        return 1
    return 0


def _self_check():
    assert merge_librenms("", "42") == ("librenms:42", "updated")
    assert merge_librenms("zabbix:7", "42") == ("librenms:42 zabbix:7", "updated")
    assert merge_librenms("librenms:42 zabbix:7", "42") == (None, "unchanged")
    assert merge_librenms("librenms:9", "42") == (None, "kept")
    assert merge_librenms("see ticket", "42") == (None, "unreadable")
    assert merge_librenms('{"zabbix": "7"}', "42") == ("librenms:42 zabbix:7", "updated")
    assert legacy_librenms({"librenms_id": 42, "librenms_device_id": 42}) == ("42", "")
    assert legacy_librenms({"librenms_id": 1, "librenms_device_id": 2}) == ("", "conflict")
    assert legacy_librenms({"librenms_device_id": 7}) == ("7", "")


def main(argv):
    parser = argparse.ArgumentParser(description="Copy NetBox librenms_id into destination as librenms:<id>.")
    add_common_args(parser)
    parser.add_argument("--self-check", action="store_true", help="check the copy rules and exit")
    args = parser.parse_args(argv)
    if args.self_check:
        _self_check()
        print("self-check ok")
        return 0
    try:
        nb = open_client(args)
        return migrate(nb, args.dry_run)
    except (APIError, RuntimeError) as exc:
        print(str(exc), file=sys.stderr)
        return 1 if isinstance(exc, APIError) else 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
