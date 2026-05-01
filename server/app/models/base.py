from sqlalchemy import text

# local time
LOCALTIME = text("(datetime('now', 'localtime'))")
