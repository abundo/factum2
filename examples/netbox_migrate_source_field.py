# Copy the legacy NetBox custom fields "source" and "source_id" into the
# single "source" field as "<system>:<id>" (factum:42, becs:17).
#
# The old source_id value is left in place. Delete the source_id custom
# field in the NetBox UI after this script has run and you have checked
# a few objects.
#
# Run it by piping this file into NetBox's nbshell. Django executes stdin
# as code and then exits:
#
#   cd /opt/netbox
#   ./venv/bin/python netbox/manage.py nbshell < netbox_migrate_source_field.py
#
# From a host, with the script on the host and NetBox in a container:
#
#   docker compose exec -T netbox \
#     /opt/netbox/venv/bin/python /opt/netbox/netbox/manage.py nbshell \
#     < netbox_migrate_source_field.py
#
# Safe to run more than once. A source value that already contains a colon
# is left alone, so a second run does not turn factum:42 into factum:42:42.
# Rows are updated with a SQL UPDATE, so NetBox webhooks and the changelog
# do not see each rewrite.

from django.db import transaction


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


def _combined_source(source, source_id):
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


def migrate_source_field():
    from extras.models import CustomField

    try:
        source_id_field = CustomField.objects.get(name="source_id")
    except CustomField.DoesNotExist:
        print("custom field source_id is not defined; nothing to copy")
        return
    try:
        source_field = CustomField.objects.get(name="source")
    except CustomField.DoesNotExist:
        print("custom field source is not defined; create it with factum2-netbox check --update, then re-run")
        return

    models = {}
    for custom_field in (source_field, source_id_field):
        for model in _assigned_models(custom_field):
            models[(model._meta.app_label, model._meta.model_name)] = model

    updated = 0
    skipped = 0
    with transaction.atomic():
        for (app_label, model_name), model in sorted(models.items()):
            label = "%s.%s" % (app_label, model_name)
            model_updated = 0
            model_skipped = 0
            rows = model.objects.filter(custom_field_data__has_key="source_id").iterator()
            for obj in rows:
                data = dict(obj.custom_field_data or {})
                new_source = _combined_source(data.get("source"), data.get("source_id"))
                if new_source is None:
                    model_skipped += 1
                    continue
                data["source"] = new_source
                model.objects.filter(pk=obj.pk).update(custom_field_data=data)
                model_updated += 1
            updated += model_updated
            skipped += model_skipped
            print("%s: updated %d, left unchanged %d" % (label, model_updated, model_skipped))

    print("done: updated %d, left unchanged %d" % (updated, skipped))
    print("source_id was not removed. Delete that custom field in the NetBox UI when you are satisfied with the new source values.")


migrate_source_field()
