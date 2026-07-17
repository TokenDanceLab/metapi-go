package backup

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// AllTables is the full backup/export order, with parent tables before
// children so imports can replay it safely.
var AllTables = []string{
	"sites",
	"site_api_endpoints",
	"site_disabled_models",
	"accounts",
	"account_tokens",
	"checkin_logs",
	"model_availability",
	"token_model_availability",
	"token_routes",
	"route_group_sources",
	"oauth_route_units",
	"oauth_route_unit_members",
	"route_channels",
	"proxy_logs",
	"proxy_debug_traces",
	"proxy_debug_attempts",
	"proxy_video_tasks",
	"admin_background_tasks",
	"proxy_files",
	"settings",
	"admin_snapshots",
	"analytics_projection_checkpoints",
	"site_day_usage",
	"site_hour_usage",
	"model_day_usage",
	"downstream_api_keys",
	"site_announcements",
	"events",
}

// AccountsTables is the subset exported when type=accounts.
var AccountsTables = []string{
	"sites",
	"site_api_endpoints",
	"site_disabled_models",
	"accounts",
	"account_tokens",
	"checkin_logs",
	"model_availability",
	"token_model_availability",
	"token_routes",
	"route_group_sources",
	"oauth_route_units",
	"oauth_route_unit_members",
	"route_channels",
	"proxy_video_tasks",
	"admin_background_tasks",
	"proxy_files",
	"downstream_api_keys",
	"site_announcements",
}

var ErrInvalidExportType = errors.New("导出类型无效，仅支持 all/accounts/preferences")

var (
	MaxExportRowsPerTable = 50_000
	MaxExportCellBytes    = 4 << 20
	MaxExportPayloadBytes = int64(64 << 20)
)

type ExportLimitError struct {
	message string
}

func (e ExportLimitError) Error() string {
	return e.message
}

func BuildPayload(db *sqlx.DB, exportType string) (map[string]any, error) {
	exportType, tables, err := TablesForExportType(exportType)
	if err != nil {
		return nil, err
	}

	result := map[string]any{}
	estimatedPayloadBytes := int64(512)
	for _, table := range tables {
		if err := addEstimatedPayloadBytes(&estimatedPayloadBytes, int64(len(table)+16)); err != nil {
			return nil, err
		}
		rows, err := queryTableAsJSON(db, table, &estimatedPayloadBytes)
		if err != nil {
			return nil, fmt.Errorf("导出失败：无法读取表 %s：%w", table, err)
		}
		result[table] = rows
	}

	return map[string]any{
		"metadata": map[string]any{
			"exported_at": time.Now().UTC().Format(time.RFC3339Nano),
			"version":     "1.0",
		},
		"type":   exportType,
		"tables": result,
	}, nil
}

func TablesForExportType(exportType string) (string, []string, error) {
	normalized := strings.TrimSpace(strings.ToLower(exportType))
	if normalized == "" {
		normalized = "all"
	}

	switch normalized {
	case "all":
		return normalized, AllTables, nil
	case "accounts":
		return normalized, AccountsTables, nil
	case "preferences":
		return normalized, []string{"settings"}, nil
	default:
		return "", nil, ErrInvalidExportType
	}
}

func QueryTableAsJSON(db *sqlx.DB, table string) ([]map[string]any, error) {
	return queryTableAsJSON(db, table, nil)
}

func queryTableAsJSON(db *sqlx.DB, table string, estimatedPayloadBytes *int64) ([]map[string]any, error) {
	if !IsKnownTable(table) {
		return nil, fmt.Errorf("unknown table: %s", table)
	}

	rows, err := db.Queryx(fmt.Sprintf("SELECT * FROM %s", quoteIdentifier(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		if MaxExportRowsPerTable > 0 && len(result) >= MaxExportRowsPerTable {
			return nil, ExportLimitError{
				message: fmt.Sprintf("表 %s 导出行数超过上限 %d", table, MaxExportRowsPerTable),
			}
		}
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			return nil, err
		}
		for k, v := range row {
			if b, ok := v.([]byte); ok {
				row[k] = string(b)
			}
			if err := validateExportCell(table, k, row[k]); err != nil {
				return nil, err
			}
		}
		if estimatedPayloadBytes != nil {
			if err := addEstimatedPayloadBytes(estimatedPayloadBytes, estimateJSONRowBytes(row)+1); err != nil {
				return nil, err
			}
		}
		result = append(result, row)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, rows.Err()
}

func validateExportCell(table string, column string, value any) error {
	if MaxExportCellBytes <= 0 {
		return nil
	}
	switch v := value.(type) {
	case string:
		if len(v) > MaxExportCellBytes {
			return ExportLimitError{
				message: fmt.Sprintf("表 %s 字段 %s 超过 %d 字节导出上限", table, column, MaxExportCellBytes),
			}
		}
	case []byte:
		if len(v) > MaxExportCellBytes {
			return ExportLimitError{
				message: fmt.Sprintf("表 %s 字段 %s 超过 %d 字节导出上限", table, column, MaxExportCellBytes),
			}
		}
	}
	return nil
}

func estimateJSONRowBytes(row map[string]any) int64 {
	if len(row) == 0 {
		return 2
	}
	var total int64 = 2 // object braces
	first := true
	for key, value := range row {
		if !first {
			total++ // comma
		}
		first = false
		total += estimateJSONStringBytes(key)
		total++ // colon
		total += estimateJSONValueBytes(value)
	}
	return total
}

func estimateJSONValueBytes(value any) int64 {
	switch v := value.(type) {
	case nil:
		return 4
	case bool:
		if v {
			return 4
		}
		return 5
	case string:
		return estimateJSONStringBytes(v)
	case []byte:
		return estimateJSONStringBytes(string(v))
	case int:
		return estimateSignedDecimalBytes(int64(v))
	case int8:
		return estimateSignedDecimalBytes(int64(v))
	case int16:
		return estimateSignedDecimalBytes(int64(v))
	case int32:
		return estimateSignedDecimalBytes(int64(v))
	case int64:
		return estimateSignedDecimalBytes(v)
	case uint:
		return estimateUnsignedDecimalBytes(uint64(v))
	case uint8:
		return estimateUnsignedDecimalBytes(uint64(v))
	case uint16:
		return estimateUnsignedDecimalBytes(uint64(v))
	case uint32:
		return estimateUnsignedDecimalBytes(uint64(v))
	case uint64:
		return estimateUnsignedDecimalBytes(v)
	default:
		return 128
	}
}

func estimateJSONStringBytes(value string) int64 {
	var total int64 = 2 // quotes
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '"', '\\':
			total += 2
		case '\b', '\f', '\n', '\r', '\t':
			total += 2
		case '<', '>', '&':
			total += 6
		default:
			if value[i] < 0x20 {
				total += 6
			} else {
				total++
			}
		}
	}
	return total
}

func estimateSignedDecimalBytes(value int64) int64 {
	if value < 0 {
		if value == -1<<63 {
			return 20
		}
		return 1 + estimateUnsignedDecimalBytes(uint64(-value))
	}
	return estimateUnsignedDecimalBytes(uint64(value))
}

func estimateUnsignedDecimalBytes(value uint64) int64 {
	var digits int64 = 1
	for value >= 10 {
		value /= 10
		digits++
	}
	return digits
}

func addEstimatedPayloadBytes(total *int64, delta int64) error {
	if total == nil {
		return nil
	}
	*total += delta
	if MaxExportPayloadBytes > 0 && *total > MaxExportPayloadBytes {
		return ExportLimitError{
			message: fmt.Sprintf("备份导出超过 %d 字节上限", MaxExportPayloadBytes),
		}
	}
	return nil
}

func IsKnownTable(name string) bool {
	for _, table := range AllTables {
		if table == name {
			return true
		}
	}
	return false
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
