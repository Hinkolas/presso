package jobs

import (
	"database/sql"
	"time"
)

func sqlTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }
