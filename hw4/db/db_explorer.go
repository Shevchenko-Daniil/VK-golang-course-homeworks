package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type ColumnInfo struct {
	Name         string
	Type         string // "string", "int", "float"
	IsNullable   bool
	IsPrimaryKey bool
	IsAutoInc    bool
}

type TableInfo struct {
	Name       string
	PrimaryKey string
	Columns    []ColumnInfo
	ColMap     map[string]ColumnInfo
}

type DBExplorer struct {
	db         *sql.DB
	tables     map[string]*TableInfo
	tableNames []string
}

func NewDBExplorer(db *sql.DB) (http.Handler, error) {
	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		return nil, err
	}

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			rows.Close()
			return nil, err
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	tables := make(map[string]*TableInfo)

	for _, tableName := range tableNames {
		colRows, err := db.Query(fmt.Sprintf("SHOW FULL COLUMNS FROM `%s`", tableName))
		if err != nil {
			return nil, err
		}

		tInfo := &TableInfo{
			Name:    tableName,
			ColMap:  make(map[string]ColumnInfo),
			Columns: make([]ColumnInfo, 0),
		}

		for colRows.Next() {
			var field, colType, collation, nullStr, key, defaultVal, extra, privileges, comment sql.NullString
			if err := colRows.Scan(&field, &colType, &collation, &nullStr, &key, &defaultVal, &extra, &privileges, &comment); err != nil {
				colRows.Close()
				return nil, err
			}

			col := ColumnInfo{
				Name:         field.String,
				Type:         parseColType(colType.String),
				IsNullable:   nullStr.String == "YES",
				IsPrimaryKey: key.String == "PRI",
				IsAutoInc:    strings.Contains(extra.String, "auto_increment"),
			}

			if col.IsPrimaryKey {
				tInfo.PrimaryKey = col.Name
			}

			tInfo.Columns = append(tInfo.Columns, col)
			tInfo.ColMap[col.Name] = col
		}
		if err := colRows.Err(); err != nil {
			colRows.Close()
			return nil, err
		}
		colRows.Close()

		tables[tableName] = tInfo
	}

	explorer := &DBExplorer{
		db:         db,
		tables:     tables,
		tableNames: tableNames,
	}

	mux := http.NewServeMux()

	// GET / - список всех таблиц ({$} означает точное совпадение корня)
	mux.HandleFunc("GET /{$}", explorer.handleGetTables)

	// Операции со всей таблицей (дублируем с трейлинг-слэшем для совместимости с PUT /items/)
	mux.HandleFunc("GET /{table}", explorer.handleGetRecords)
	mux.HandleFunc("GET /{table}/", explorer.handleGetRecords)
	mux.HandleFunc("PUT /{table}", explorer.handleCreateRecord)
	mux.HandleFunc("PUT /{table}/", explorer.handleCreateRecord)

	// Операции с отдельной записью по ID
	mux.HandleFunc("GET /{table}/{id}", explorer.handleGetRecord)
	mux.HandleFunc("POST /{table}/{id}", explorer.handleUpdateRecord)
	mux.HandleFunc("DELETE /{table}/{id}", explorer.handleDeleteRecord)

	return mux, nil
}

func parseColType(t string) string {
	t = strings.ToLower(t)
	if strings.Contains(t, "int") {
		return "int"
	}
	if strings.Contains(t, "float") || strings.Contains(t, "double") || strings.Contains(t, "decimal") {
		return "float"
	}
	return "string"
}

func (e *DBExplorer) getTable(w http.ResponseWriter, r *http.Request) (*TableInfo, bool) {
	name := r.PathValue("table")
	table, ok := e.tables[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "unknown table"})
		return nil, false
	}
	return table, true
}

func (e *DBExplorer) handleGetTables(w http.ResponseWriter, r *http.Request) {
	err := e.updateDBExplorer()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"response": map[string]interface{}{
			"tables": e.tableNames,
		},
	})
}

func (e *DBExplorer) handleGetRecords(w http.ResponseWriter, r *http.Request) {
	err := e.updateDBExplorer()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	table, ok := e.getTable(w, r)
	if !ok {
		return
	}

	limit := 5
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l >= 0 {
			limit = l
		}
	}

	offset := 0
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil && o >= 0 {
			offset = o
		}
	}

	cols := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = fmt.Sprintf("`%s`", col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM `%s` LIMIT ? OFFSET ?", strings.Join(cols, ", "), table.Name)
	rows, err := e.db.Query(query, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer rows.Close()

	records := make([]map[string]interface{}, 0)
	for rows.Next() {
		vals := make([]interface{}, len(table.Columns))
		ptrs := make([]interface{}, len(table.Columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}

		rec := make(map[string]interface{})
		for i, col := range table.Columns {
			rec[col.Name] = convertValue(vals[i], col)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"response": map[string]interface{}{
			"records": records,
		},
	})
}

func (e *DBExplorer) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	err := e.updateDBExplorer()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	table, ok := e.getTable(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")

	cols := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = fmt.Sprintf("`%s`", col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM `%s` WHERE `%s` = ?", strings.Join(cols, ", "), table.Name, table.PrimaryKey)

	vals := make([]interface{}, len(table.Columns))
	ptrs := make([]interface{}, len(table.Columns))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	err = e.db.QueryRow(query, id).Scan(ptrs...)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "record not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	record := make(map[string]interface{})
	for i, col := range table.Columns {
		record[col.Name] = convertValue(vals[i], col)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"response": map[string]interface{}{
			"record": record,
		},
	})
}

func (e *DBExplorer) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	err := e.updateDBExplorer()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	table, ok := e.getTable(w, r)
	if !ok {
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})
		return
	}

	for _, col := range table.Columns {
		if col.IsPrimaryKey && col.IsAutoInc {
			continue
		}

		val, exists := data[col.Name]
		if exists && !validateField(col, val) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": fmt.Sprintf("field %s have invalid type", col.Name),
			})
			return
		}
	}

	var insertCols []string
	var placeholders []string
	var args []interface{}

	for _, col := range table.Columns {
		if col.IsPrimaryKey && col.IsAutoInc {
			continue
		}

		val, exists := data[col.Name]
		insertCols = append(insertCols, fmt.Sprintf("`%s`", col.Name))
		placeholders = append(placeholders, "?")

		if exists {
			args = append(args, val)
		} else {
			if col.IsNullable {
				args = append(args, nil)
			} else {
				switch col.Type {
				case "int":
					args = append(args, 0)
				case "float":
					args = append(args, 0.0)
				default:
					args = append(args, "")
				}
			}
		}
	}

	query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		table.Name,
		strings.Join(insertCols, ", "),
		strings.Join(placeholders, ", "),
	)

	res, err := e.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	var respID interface{} = lastID
	if lastID == 0 && table.PrimaryKey != "" {
		if v, ok := data[table.PrimaryKey]; ok {
			respID = v
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"response": map[string]interface{}{
			table.PrimaryKey: respID,
		},
	})
}

func (e *DBExplorer) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	err := e.updateDBExplorer()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	table, ok := e.getTable(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})
		return
	}

	if _, exists := data[table.PrimaryKey]; exists {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": fmt.Sprintf("field %s have invalid type", table.PrimaryKey),
		})
		return
	}

	for _, col := range table.Columns {
		val, exists := data[col.Name]
		if !exists {
			continue
		}
		if !validateField(col, val) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": fmt.Sprintf("field %s have invalid type", col.Name),
			})
			return
		}
	}

	var setClauses []string
	var args []interface{}

	for _, col := range table.Columns {
		if col.Name == table.PrimaryKey {
			continue
		}
		val, exists := data[col.Name]
		if !exists {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("`%s` = ?", col.Name))
		args = append(args, val)
	}

	if len(setClauses) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"response": map[string]interface{}{
				"updated": 0,
			},
		})
		return
	}

	query := fmt.Sprintf("UPDATE `%s` SET %s WHERE `%s` = ?",
		table.Name,
		strings.Join(setClauses, ", "),
		table.PrimaryKey,
	)
	args = append(args, id)

	res, err := e.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"response": map[string]interface{}{
			"updated": rowsAffected,
		},
	})
}

func (e *DBExplorer) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	err := e.updateDBExplorer()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	table, ok := e.getTable(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")

	query := fmt.Sprintf("DELETE FROM `%s` WHERE `%s` = ?", table.Name, table.PrimaryKey)

	res, err := e.db.Exec(query, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"response": map[string]interface{}{
			"deleted": rowsAffected,
		},
	})
}

func (e *DBExplorer) updateDBExplorer() error {
	db := e.db

	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		return err
	}

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			rows.Close()
			return err
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tables := make(map[string]*TableInfo)

	for _, tableName := range tableNames {
		colRows, err := db.Query(fmt.Sprintf("SHOW FULL COLUMNS FROM `%s`", tableName))
		if err != nil {
			return err
		}

		tInfo := &TableInfo{
			Name:    tableName,
			ColMap:  make(map[string]ColumnInfo),
			Columns: make([]ColumnInfo, 0),
		}

		for colRows.Next() {
			var field, colType, collation, nullStr, key, defaultVal, extra, privileges, comment sql.NullString
			if err := colRows.Scan(&field, &colType, &collation, &nullStr, &key, &defaultVal, &extra, &privileges, &comment); err != nil {
				colRows.Close()
				return err
			}

			col := ColumnInfo{
				Name:         field.String,
				Type:         parseColType(colType.String),
				IsNullable:   nullStr.String == "YES",
				IsPrimaryKey: key.String == "PRI",
				IsAutoInc:    strings.Contains(extra.String, "auto_increment"),
			}

			if col.IsPrimaryKey {
				tInfo.PrimaryKey = col.Name
			}

			tInfo.Columns = append(tInfo.Columns, col)
			tInfo.ColMap[col.Name] = col
		}
		if err := colRows.Err(); err != nil {
			colRows.Close()
			return err
		}
		colRows.Close()

		tables[tableName] = tInfo
	}

	e.db = db
	e.tables = tables
	e.tableNames = tableNames

	return nil

}

func validateField(col ColumnInfo, val interface{}) bool {
	if val == nil {
		return col.IsNullable
	}
	switch col.Type {
	case "string":
		_, ok := val.(string)
		return ok
	case "int":
		switch v := val.(type) {
		case float64:
			return v == float64(int64(v))
		case int, int64, int32:
			return true
		default:
			return false
		}
	case "float":
		switch val.(type) {
		case float64, float32, int, int64:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func convertValue(val interface{}, col ColumnInfo) interface{} {
	if val == nil {
		return nil
	}
	switch col.Type {
	case "int":
		switch v := val.(type) {
		case int64:
			return v
		case []byte:
			i, _ := strconv.ParseInt(string(v), 10, 64)
			return i
		default:
			return val
		}
	case "float":
		switch v := val.(type) {
		case float64:
			return v
		case []byte:
			f, _ := strconv.ParseFloat(string(v), 64)
			return f
		default:
			return val
		}
	case "string":
		switch v := val.(type) {
		case []byte:
			return string(v)
		case string:
			return v
		default:
			return fmt.Sprintf("%v", v)
		}
	default:
		if b, ok := val.([]byte); ok {
			return string(b)
		}
		return val
	}
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
