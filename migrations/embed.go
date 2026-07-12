package migrations

import _ "embed"

//go:embed postgres/000001_init.up.sql
var PostgreSQL string

//go:embed postgres/000002_collection_groups.up.sql
var PostgreSQLGroups string

//go:embed postgres/000003_write_points_simplify.up.sql
var PostgreSQLWritePoints string

//go:embed tdengine/000001_init.sql
var TDengine string
