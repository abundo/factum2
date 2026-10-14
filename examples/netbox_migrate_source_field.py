#!/usr/bin/env python3
# Copy legacy NetBox origin fields into the single "source" field.
#
# Two passes, in this order:
#
#   1. source + source_id  ->  source "<system>:<id>"   (factum:42)
#      A source value that already contains a colon is left alone, so a
#      second run does not turn factum:42 into factum:42:42.
#   2. becs_oid            ->  source "becs:<oid>"
#      An origin that is not BECS (for example factum:42) is left alone.
#
# The old values are left in place. Delete source_id and becs_oid in the
# NetBox UI after you have checked a few objects.
#
# Create or extend source first:
#
#   factum2-netbox check --update
#
# NetBox URL and token are read from the factum settings row. The database
# connection comes from factum2.yaml (default /etc/factum2/factum2.yaml).
#
#   python3 examples/netbox_migrate_source_field.py
#   python3 examples/netbox_migrate_source_field.py --dry-run
#
# Safe to run more than once. Each write is a PATCH of custom_fields.source
# only. A PATCH does fire the object_updated webhook.

import argparse
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
    parse_factum_db,
    patch_custom_field,
    row_label,
)

SOURCE_FIELD = "source"
SOURCE_ID_FIELD = "source_id"
BECS_FIELD = "becs_oid"
BECS_SYSTEM = "becs"


def _cf_text(value):
    """Text form of a custom-field value. Empty when there is nothing to copy."""
    if value is None or isinstance(value, bool):
        return ""
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        if value != value:  # NaN
            return ""
        if value.is_integer():
            return str(int(value))
        return str(value).strip()
    if isinstance(value, str):
        return value.strip()
    return ""


def combined_source(source, source_id):
    """Return the new source value, or None when this row should be skipped.

    None means: nothing to copy, or source is already "<system>:<id>".
    """
    system = _cf_text(source)
    ident = _cf_text(source_id)
    if system == "" or ident == "":
        return None
    if ":" in system:
        return None
    return system + ":" + ident


def migrate_source_id(nb, dry_run):
    source = find_field(nb, SOURCE_FIELD)
    if source is None:
        print("custom field source is not defined; create it with factum2-netbox check --update, then re-run")
        return 1
    legacy = find_field(nb, SOURCE_ID_FIELD)
    if legacy is None:
        print("custom field source_id is not defined; nothing to copy")
        return 0
    resolve = content_type_resolver(nb)
    source_types = set(object_types(source, resolve))
    types = object_types(legacy, resolve)
    if not types:
        print("source_id is not assigned to any object type; nothing to copy")
        return 0

    updated = 0
    skipped = 0
    failed = False
    for obj_type in types:
        path = OBJECT_PATHS.get(obj_type)
        if path is None:
            print("skip %s: no REST path for this object type" % obj_type)
            failed = True
            continue
        if obj_type not in source_types:
            print("%s is not assigned the source field; run factum2-netbox check --update" % obj_type)
            failed = True
            continue
        model_updated = 0
        model_skipped = 0
        seen = set()
        for row in iter_filtered(nb, path, SOURCE_ID_FIELD, "empty"):
            rid = row.get("id")
            if rid in seen:
                continue
            seen.add(rid)
            fields = row.get("custom_fields") or {}
            new_source = combined_source(fields.get(SOURCE_FIELD), fields.get(SOURCE_ID_FIELD))
            if new_source is None:
                model_skipped += 1
                continue
            label = row_label(row)
            if dry_run:
                print("dry-run: would set %s %s source=%s" % (obj_type, label, new_source))
            else:
                patch_custom_field(nb, path, row, SOURCE_FIELD, new_source)
                print("updated %s %s source=%s" % (obj_type, label, new_source))
            model_updated += 1
        updated += model_updated
        skipped += model_skipped
        print("%s: updated %d, left unchanged %d" % (obj_type, model_updated, model_skipped))

    print("source_id: updated %d, left unchanged %d" % (updated, skipped))
    print("source_id was not removed. Delete that custom field in the NetBox UI when you are satisfied with source.")
    if failed:
        return 1
    return 0


def _oid_text(value):
    """Text form of a positive integer custom field. Empty otherwise."""
    if value is None or isinstance(value, bool):
        return ""
    if isinstance(value, int):
        if value <= 0:
            return ""
        return str(value)
    if isinstance(value, float):
        if value != value or value <= 0:  # NaN or not a positive oid
            return ""
        if value.is_integer():
            return str(int(value))
        return ""
    if isinstance(value, str):
        text = value.strip()
        if text == "" or text == "0":
            return ""
        if text.endswith(".0") and text[:-2].isdigit():
            return text[:-2]
        return text
    return ""


def positive_oid(value):
    text = _oid_text(value)
    if text == "" or not text.isdigit() or text == "0":
        return ""
    return text


def classify_becs(source, oid):
    """Return (new_value_or_None, status) for copying becs_oid into source.

    status is updated, unchanged, conflict, kept, or unreadable.
    updated carries the becs:<oid> string to write.
    """
    if not isinstance(oid, str) or not oid.isdigit() or oid == "0":
        return None, "skip"
    want = BECS_SYSTEM + ":" + oid
    if source is None or source == "":
        return want, "updated"
    if isinstance(source, (dict, list, bool, int, float)):
        return None, "unreadable"
    if not isinstance(source, str):
        return None, "unreadable"
    text = source.strip()
    if text == "":
        return want, "updated"
    if any(ch.isspace() for ch in text):
        return None, "unreadable"
    system, sep, ident = text.partition(":")
    system = system.strip().lower()
    ident = ident.strip()
    if sep == "" or system == "":
        return None, "kept"
    if system == BECS_SYSTEM:
        if ident == oid:
            return None, "unchanged"
        if ident == "":
            return want, "updated"
        return None, "conflict"
    return None, "kept"


def migrate_becs_oid(nb, dry_run):
    source = find_field(nb, SOURCE_FIELD)
    if source is None:
        print("custom field source is not defined; create it with factum2-netbox check --update, then re-run")
        return 1
    if source.get("type") not in (None, "", "text"):
        print("custom field source has type %r, want text" % (source.get("type"),))
        return 1
    resolve = content_type_resolver(nb)
    source_types = set(object_types(source, resolve))

    legacy = find_field(nb, BECS_FIELD)
    if legacy is None:
        print("custom field becs_oid is not defined; nothing to copy")
        return 0
    legacy_types = object_types(legacy, resolve)
    if not legacy_types:
        print("becs_oid is not assigned to any object type; nothing to copy")
        return 0

    counts = {"updated": 0, "unchanged": 0, "conflict": 0, "kept": 0, "unreadable": 0, "skip": 0}
    failed = False
    for obj_type in legacy_types:
        path = OBJECT_PATHS.get(obj_type)
        if path is None:
            print("skip %s: no REST path for this object type" % obj_type)
            failed = True
            continue
        if obj_type not in source_types:
            print("%s is not assigned the source field; run factum2-netbox check --update" % obj_type)
            failed = True
            continue
        model_counts = {key: 0 for key in counts}
        seen = set()
        for row in iter_filtered(nb, path, BECS_FIELD, "gt"):
            rid = row.get("id")
            if rid in seen:
                continue
            seen.add(rid)
            fields = row.get("custom_fields") or {}
            oid = positive_oid(fields.get(BECS_FIELD))
            if oid == "":
                continue
            new_value, status = classify_becs(fields.get(SOURCE_FIELD), oid)
            if status == "updated":
                label = row_label(row)
                if dry_run:
                    print("dry-run: would set %s %s source=%s" % (obj_type, label, new_value))
                else:
                    patch_custom_field(nb, path, row, SOURCE_FIELD, new_value)
                    print("updated %s %s source=%s" % (obj_type, label, new_value))
            elif status == "conflict":
                print("%s %s: source is %r, becs_oid is %s; left unchanged" % (
                    obj_type, row_label(row), fields.get(SOURCE_FIELD), oid))
            elif status == "kept":
                print("%s %s: source is %r (not BECS); left unchanged" % (
                    obj_type, row_label(row), fields.get(SOURCE_FIELD)))
            elif status == "unreadable":
                print("%s %s: source value is not readable; left unchanged" % (obj_type, row_label(row)))
            model_counts[status] += 1
        for key, n in model_counts.items():
            counts[key] += n
        print("%s: updated %d, unchanged %d, conflicts %d, kept other source %d, unreadable %d" % (
            obj_type,
            model_counts["updated"],
            model_counts["unchanged"],
            model_counts["conflict"],
            model_counts["kept"],
            model_counts["unreadable"],
        ))

    print("becs_oid: updated %d, unchanged %d, conflicts %d, kept other source %d, unreadable %d" % (
        counts["updated"], counts["unchanged"], counts["conflict"], counts["kept"], counts["unreadable"]))
    print("becs_oid was not removed. Delete that custom field in the NetBox UI when you are satisfied with source.")
    if failed or counts["conflict"] or counts["unreadable"]:
        return 1
    return 0


def _self_check():
    assert combined_source("factum", 42) == "factum:42"
    assert combined_source("factum", "17") == "factum:17"
    assert combined_source("factum:42", "99") is None
    assert combined_source("", "17") is None
    assert combined_source("factum", "") is None
    assert combined_source("becs", 3641645) == "becs:3641645"
    assert classify_becs(None, "3641645") == ("becs:3641645", "updated")
    assert classify_becs("", "3641645") == ("becs:3641645", "updated")
    assert classify_becs("  ", "3641645") == ("becs:3641645", "updated")
    assert classify_becs("becs:3641645", "3641645") == (None, "unchanged")
    assert classify_becs("BECS:3641645", "3641645") == (None, "unchanged")
    assert classify_becs("becs:", "3641645") == ("becs:3641645", "updated")
    assert classify_becs("becs:9", "3641645") == (None, "conflict")
    assert classify_becs("factum:42", "3641645") == (None, "kept")
    assert classify_becs("factum", "3641645") == (None, "kept")
    assert classify_becs("becs:3641645 extra", "3641645") == (None, "unreadable")
    assert classify_becs({"becs": "1"}, "3641645") == (None, "unreadable")
    assert classify_becs(None, "") == (None, "skip")
    assert classify_becs(None, "0") == (None, "skip")
    assert positive_oid(3641645) == "3641645"
    assert positive_oid(3641645.0) == "3641645"
    assert positive_oid("3641645") == "3641645"
    assert positive_oid(0) == ""
    assert positive_oid("nope") == ""
    assert object_types({"object_types": ["dcim.device", "dcim.device", "ipam.ipaddress"]}) == [
        "dcim.device", "ipam.ipaddress"]
    assert object_types({"content_types": [{"app_label": "dcim", "model": "interface"}]}) == [
        "dcim.interface"]
    sample = """
factum:
    url: https://factum.example.com
db:
    host: 127.0.0.1
    port: 5432
    user: factum2_user
    pass: "s:ec ret" # keep
    database: factum2
web:
    bind: 127.0.0.1:8091
"""
    db = parse_factum_db(sample)
    assert db == {
        "host": "127.0.0.1",
        "port": "5432",
        "user": "factum2_user",
        "pass": "s:ec ret",
        "database": "factum2",
    }


def main(argv):
    parser = argparse.ArgumentParser(description="Copy NetBox source_id and becs_oid into source.")
    add_common_args(parser)
    parser.add_argument("--self-check", action="store_true", help="check the copy rules and exit")
    args = parser.parse_args(argv)
    if args.self_check:
        _self_check()
        print("self-check ok")
        return 0
    try:
        nb = open_client(args)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    try:
        print("pass 1: source + source_id")
        first = migrate_source_id(nb, args.dry_run)
        print("pass 2: becs_oid")
        second = migrate_becs_oid(nb, args.dry_run)
    except APIError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return 1 if first or second else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
