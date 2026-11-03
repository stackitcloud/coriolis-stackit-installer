import configparser
import hashlib
import json
import sys

import sqlalchemy as sa

config = configparser.ConfigParser(interpolation=None, strict=False)
config.read("/etc/coriolis/coriolis.conf")
engine = sa.create_engine(config.get("database", "connection"))
if engine.url.host not in ("localhost", "127.0.0.1", "::1", "mariadb", sys.argv[1]):
    raise ValueError("external database is not covered by appliance volume snapshots")
metadata = sa.MetaData()
inventory = {}
with engine.connect() as connection:
    for name in ("endpoint", "base_transfer_action", "transfer_schedules"):
        table = sa.Table(name, metadata, autoload_with=engine)
        rows = [dict(row._mapping) for row in connection.execute(
            sa.select(table).where(table.c.deleted == 0))]
        encoded = sorted(json.dumps(row, sort_keys=True, default=str) for row in rows)
        inventory[name] = hashlib.sha256(json.dumps(encoded).encode()).hexdigest()
    executions = sa.Table("tasks_execution", metadata, autoload_with=engine)
    terminal = ("COMPLETED", "ERROR", "CANCELED", "CANCELLED")
    active = connection.execute(sa.select(sa.func.count()).select_from(executions).where(
        executions.c.deleted == 0,
        sa.or_(executions.c.status.is_(None), sa.func.upper(executions.c.status).notin_(terminal)),
    )).scalar_one()
    schedules = metadata.tables["transfer_schedules"]
    enabled = connection.execute(sa.select(sa.func.count()).select_from(schedules).where(
        schedules.c.deleted == 0, schedules.c.enabled == True,
    )).scalar_one()
print(json.dumps({"inventory": inventory, "active_executions": active, "enabled_schedules": enabled}))
