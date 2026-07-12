package api

import (
	"aquacontrolai/internal/model"
	"aquacontrolai/internal/pkg/response"
	postgres "aquacontrolai/internal/repository/postgres"
	"aquacontrolai/internal/service/platform"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	Platform *platform.Service
	History  *platform.History
}

func NewRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), requestLog())
	v := r.Group("/api/v1")
	v.GET("/health", h.health)
	v.GET("/devices/protocols", h.protocols)
	v.GET("/devices", h.devices)
	v.POST("/devices/export", h.exportConfig("device"))
	v.POST("/devices/import", h.importConfig("device"))
	v.POST("/devices", h.createDevice)
	v.GET("/devices/:id", h.deviceDetail)
	v.PUT("/devices/:id", h.updateDevice)
	v.DELETE("/devices/:id", h.deleteDevice)
	v.GET("/collection-points", h.points("collection"))
	v.GET("/collection-points/groups", h.groups)
	v.POST("/collection-points/groups", h.createGroup)
	v.PUT("/collection-points/groups", h.updateGroup)
	v.DELETE("/collection-points/groups/:name", h.deleteGroup)
	v.POST("/collection-points/export", h.exportConfig("collection"))
	v.POST("/collection-points/import", h.importConfig("collection"))
	v.GET("/collection-points/:id", h.pointDetail("collection"))
	v.POST("/collection-points", h.savePoint("collection", false))
	v.PUT("/collection-points/:id", h.savePoint("collection", true))
	v.DELETE("/collection-points/:id", h.deletePoint("collection"))
	v.GET("/write-points", h.points("write"))
	v.POST("/write-points/export", h.exportConfig("write"))
	v.POST("/write-points/import", h.importConfig("write"))
	v.GET("/write-points/:id", h.pointDetail("write"))
	v.POST("/write-points", h.savePoint("write", false))
	v.PUT("/write-points/:id", h.savePoint("write", true))
	v.DELETE("/write-points/:id", h.deletePoint("write"))
	v.POST("/write-points/:id/write", h.writePoint)
	v.GET("/write-logs", h.logs)
	v.GET("/history/tree", h.tree)
	v.POST("/history/query", h.historyQuery)
	v.POST("/history/query-table", h.historyTable)
	v.POST("/history/export", h.exportHistory)
	v.GET("/system/history-retention", h.getRetention)
	v.PUT("/system/history-retention", h.setRetention)
	return r
}
func requestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("HTTP 请求", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	}
}
func (h *Handler) health(c *gin.Context) { response.OK(c, map[string]string{"status": "ok"}) }
func (h *Handler) protocols(c *gin.Context) {
	response.OK(c, map[string]any{"items": h.Platform.Registry.Metadata()})
}
func (h *Handler) devices(c *gin.Context) {
	items, e := h.Platform.ListDevices(c, c.Query("keyword"))
	if e != nil {
		serverError(c, e)
		return
	}
	filtered := make([]model.Device, 0, len(items))
	for _, d := range items {
		if v := c.Query("protocol_type"); v != "" && d.ProtocolType != v {
			continue
		}
		if v := c.Query("enabled"); v != "" && strconv.FormatBool(d.Enabled) != v {
			continue
		}
		filtered = append(filtered, d)
	}
	pageNo, pageSize := pagination(c)
	start, end := bounds(len(filtered), pageNo, pageSize)
	response.OK(c, map[string]any{"total": len(filtered), "page": pageNo, "page_size": pageSize, "items": filtered[start:end]})
}
func (h *Handler) deviceDetail(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		response.Error(c, 400, 40001, "无效ID", nil)
		return
	}
	d, e := h.Platform.GetDevice(c, id)
	if e != nil {
		response.Error(c, 404, 40004, "设备不存在", nil)
		return
	}
	response.OK(c, d)
}
func bindDevice(c *gin.Context) (model.Device, error) {
	var req struct {
		Name              string          `json:"name"`
		ProtocolType      string          `json:"protocol_type"`
		Enabled           *bool           `json:"enabled"`
		Host              string          `json:"host"`
		Port              int             `json:"port"`
		ConnectTimeout    int             `json:"connect_timeout"`
		ReconnectInterval int             `json:"reconnect_interval"`
		ProtocolConfig    json.RawMessage `json:"protocol_config"`
	}
	if e := c.ShouldBindJSON(&req); e != nil {
		return model.Device{}, e
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return model.Device{Name: req.Name, ProtocolType: req.ProtocolType, Enabled: enabled, Host: req.Host, Port: req.Port, ConnectTimeout: req.ConnectTimeout, ReconnectInterval: req.ReconnectInterval, ProtocolConfig: req.ProtocolConfig}, nil
}
func (h *Handler) createDevice(c *gin.Context) {
	d, e := bindDevice(c)
	if e == nil {
		e = h.Platform.SaveDevice(c, nil, &d)
	}
	if e != nil {
		response.Error(c, http.StatusBadRequest, 40001, e.Error(), nil)
		return
	}
	response.Created(c, d)
}
func (h *Handler) updateDevice(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	d, e2 := bindDevice(c)
	if e != nil || e2 != nil {
		response.Error(c, 400, 40001, "无效请求", nil)
		return
	}
	if e = h.Platform.SaveDevice(c, &id, &d); e != nil {
		response.Error(c, 400, 40001, e.Error(), nil)
		return
	}
	response.OK(c, d)
}
func (h *Handler) deleteDevice(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		response.Error(c, 400, 40001, "无效ID", nil)
		return
	}
	if e = h.Platform.DeleteDevice(c, id); e != nil {
		response.Error(c, 404, 40004, "设备不存在", nil)
		return
	}
	response.OK(c, nil)
}
func (h *Handler) points(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, e := h.Platform.ListPoints(c, kind, c.Query("keyword"))
		if e != nil {
			serverError(c, e)
			return
		}
		filtered := make([]postgres.PointRow, 0, len(items))
		for _, p := range items {
			if v := c.Query("device_id"); v != "" && p.DeviceID.String() != v {
				continue
			}
			if v := c.Query("group_name"); v != "" && p.GroupName != v {
				continue
			}
			if v := c.Query("data_type"); v != "" && p.DataType != v {
				continue
			}
			if v := c.Query("enabled"); v != "" && strconv.FormatBool(p.Enabled) != v {
				continue
			}
			if kind == "write" {
				if v := c.Query("write_enabled"); v != "" && strconv.FormatBool(p.WriteEnabled) != v {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		pageNo, pageSize := pagination(c)
		start, end := bounds(len(filtered), pageNo, pageSize)
		response.OK(c, map[string]any{"total": len(filtered), "page": pageNo, "page_size": pageSize, "items": filtered[start:end]})
	}
}
func pagination(c *gin.Context) (int, int) {
	pageNo := parseInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parseInt(c.DefaultQuery("page_size", "20"), 20)
	if pageNo < 1 {
		pageNo = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return pageNo, pageSize
}
func bounds(total, pageNo, pageSize int) (int, int) {
	start := (pageNo - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}
func (h *Handler) pointDetail(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, e := uuid.Parse(c.Param("id"))
		if e != nil {
			response.Error(c, 400, 41001, "无效ID", nil)
			return
		}
		p, e := h.Platform.GetPoint(c, kind, id)
		if e != nil {
			response.Error(c, 404, 41004, "点位不存在", nil)
			return
		}
		response.OK(c, p)
	}
}
func (h *Handler) groups(c *gin.Context) {
	groups, e := h.Platform.Groups(c)
	if e != nil {
		serverError(c, e)
		return
	}
	response.OK(c, map[string]any{"groups": groups})
}
func (h *Handler) createGroup(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if e := c.ShouldBindJSON(&req); e != nil {
		response.Error(c, 400, 41001, "参数格式错误", nil)
		return
	}
	if e := h.Platform.CreateGroup(c, req.Name); e != nil {
		response.Error(c, 400, 41001, e.Error(), nil)
		return
	}
	response.Created(c, map[string]string{"name": req.Name})
}
func (h *Handler) updateGroup(c *gin.Context) {
	var req struct {
		OldName string `json:"old_name"`
		Name    string `json:"name"`
	}
	if e := c.ShouldBindJSON(&req); e != nil {
		response.Error(c, 400, 41001, "参数格式错误", nil)
		return
	}
	if e := h.Platform.UpdateGroup(c, req.OldName, req.Name); e != nil {
		response.Error(c, 400, 41001, e.Error(), nil)
		return
	}
	response.OK(c, map[string]string{"name": req.Name})
}
func (h *Handler) deleteGroup(c *gin.Context) {
	name := c.Param("name")
	if e := h.Platform.DeleteGroup(c, name); e != nil {
		response.Error(c, 409, 41003, e.Error(), nil)
		return
	}
	response.OK(c, nil)
}
func (h *Handler) getRetention(c *gin.Context) {
	days, e := h.Platform.GetRetention(c)
	if e != nil {
		serverError(c, e)
		return
	}
	response.OK(c, map[string]int{"history_retention_days": days})
}
func (h *Handler) setRetention(c *gin.Context) {
	var req struct {
		Days int `json:"history_retention_days"`
	}
	if e := c.ShouldBindJSON(&req); e != nil {
		response.Error(c, 400, 43001, "参数格式错误", nil)
		return
	}
	if e := h.Platform.SetRetention(c, req.Days); e != nil {
		response.Error(c, 502, 43005, e.Error(), nil)
		return
	}
	response.OK(c, map[string]int{"history_retention_days": req.Days})
}
func (h *Handler) exportConfig(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/csv; charset=utf-8-sig")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, map[string]string{"device": "devices", "collection": "collection_points", "write": "write_points"}[kind]))
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		w := csv.NewWriter(c.Writer)
		defer w.Flush()
		if kind == "device" {
			items, e := h.Platform.ListDevices(c, "")
			if e != nil {
				return
			}
			w.Write([]string{"name", "protocol_type", "host", "port", "connect_timeout", "reconnect_interval", "protocol_config", "enabled"})
			for _, d := range items {
				w.Write([]string{d.Name, d.ProtocolType, d.Host, strconv.Itoa(d.Port), strconv.Itoa(d.ConnectTimeout), strconv.Itoa(d.ReconnectInterval), string(d.ProtocolConfig), strings.ToUpper(strconv.FormatBool(d.Enabled))})
			}
			return
		}
		items, e := h.Platform.ListPoints(c, kind, "")
		if e != nil {
			return
		}
		if kind == "collection" {
			w.Write([]string{"name", "group_name", "device_name", "address", "data_type", "unit", "collect_interval", "store_history", "history_interval", "enabled"})
			for _, p := range items {
				w.Write([]string{p.Name, p.GroupName, p.DeviceName, p.Address, p.DataType, stringValue(p.Unit), strconv.Itoa(p.CollectInterval), strings.ToUpper(strconv.FormatBool(p.StoreHistory)), strconv.Itoa(p.HistoryInterval), strings.ToUpper(strconv.FormatBool(p.Enabled))})
			}
		} else {
			w.Write([]string{"name", "group_name", "device_name", "address", "data_type", "unit", "enabled", "write_enabled", "readback_tolerance"})
			for _, p := range items {
				w.Write([]string{p.Name, p.GroupName, p.DeviceName, p.Address, p.DataType, stringValue(p.Unit), strings.ToUpper(strconv.FormatBool(p.Enabled)), strings.ToUpper(strconv.FormatBool(p.WriteEnabled)), strconv.FormatFloat(p.ReadbackTolerance, 'g', -1, 64)})
			}
		}
	}
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func floatPtr(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'g', -1, 64)
}
func (h *Handler) importConfig(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, e := c.FormFile("file")
		if e != nil {
			response.Error(c, 400, 10001, "缺少file字段", nil)
			return
		}
		f, e := file.Open()
		if e != nil {
			serverError(c, e)
			return
		}
		defer f.Close()
		rows, e := csv.NewReader(f).ReadAll()
		if e != nil || len(rows) < 1 {
			response.Error(c, 400, 10001, "CSV格式错误", nil)
			return
		}
		headers := map[string]int{}
		for i, v := range rows[0] {
			headers[strings.TrimPrefix(v, "\ufeff")] = i
		}
		result := map[string]any{"total": len(rows) - 1, "created": 0, "updated": 0, "failed": 0, "errors": []map[string]any{}}
		errs := []map[string]any{}
		for i, row := range rows[1:] {
			created, err := h.importRow(c, kind, headers, row)
			if err != nil {
				result["failed"] = result["failed"].(int) + 1
				errs = append(errs, map[string]any{"row": i + 2, "field": "row", "message": err.Error()})
			} else if created {
				result["created"] = result["created"].(int) + 1
			} else {
				result["updated"] = result["updated"].(int) + 1
			}
		}
		result["errors"] = errs
		response.OK(c, result)
	}
}
func (h *Handler) importRow(ctx context.Context, kind string, head map[string]int, row []string) (bool, error) {
	get := func(k string) string {
		i, ok := head[k]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	if kind == "device" {
		cfg := json.RawMessage(get("protocol_config"))
		enabled, e := parseCSVBool(get("enabled"), true)
		if e != nil {
			return false, e
		}
		d := model.Device{Name: get("name"), ProtocolType: get("protocol_type"), Host: get("host"), Port: parseInt(get("port"), 0), ConnectTimeout: parseInt(get("connect_timeout"), 5), ReconnectInterval: parseInt(get("reconnect_interval"), 10), ProtocolConfig: cfg, Enabled: enabled}
		existing, e := h.Platform.FindDeviceByName(ctx, d.Name)
		created := platform.IsNotFound(e)
		var id *uuid.UUID
		if !created {
			id = &existing.ID
		}
		return created, h.Platform.SaveDevice(ctx, id, &d)
	}
	device, e := h.Platform.FindDeviceByName(ctx, get("device_name"))
	if e != nil {
		return false, fmt.Errorf("device_name不存在")
	}
	existing, e := h.Platform.FindPointByName(ctx, kind, get("name"))
	created := platform.IsNotFound(e)
	enabled, be := parseCSVBool(get("enabled"), true)
	if be != nil {
		return false, be
	}
	unit := get("unit")
	p := postgres.PointRow{ID: uuid.Nil, Name: get("name"), GroupName: get("group_name"), DeviceID: device.ID, Enabled: enabled, Address: get("address"), DataType: get("data_type"), Unit: &unit}
	if !created {
		p.ID = existing.ID
	}
	if kind == "collection" {
		p.CollectInterval = parseInt(get("collect_interval"), 1)
		p.StoreHistory, _ = parseCSVBool(get("store_history"), true)
		p.HistoryInterval = parseInt(get("history_interval"), 1)
	} else {
		p.WriteEnabled, _ = parseCSVBool(get("write_enabled"), false)
		p.ReadbackTolerance = parseFloat(get("readback_tolerance"), .0001)
	}
	return created, h.Platform.SavePoint(ctx, kind, &p)
}
func parseInt(v string, d int) int {
	if v == "" {
		return d
	}
	n, e := strconv.Atoi(v)
	if e != nil {
		return d
	}
	return n
}
func parseFloat(v string, d float64) float64 {
	if v == "" {
		return d
	}
	n, e := strconv.ParseFloat(v, 64)
	if e != nil {
		return d
	}
	return n
}
func parseCSVBool(v string, d bool) (bool, error) {
	if v == "" {
		return d, nil
	}
	return strconv.ParseBool(strings.ToLower(v))
}
func (h *Handler) savePoint(kind string, update bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var p struct {
			ID                uuid.UUID `json:"-"`
			Name              string    `json:"name"`
			GroupName         string    `json:"group_name"`
			DeviceID          uuid.UUID `json:"device_id"`
			Enabled           *bool     `json:"enabled"`
			WriteEnabled      bool      `json:"write_enabled"`
			Address           string    `json:"address"`
			DataType          string    `json:"data_type"`
			Unit              *string   `json:"unit"`
			CollectInterval   int       `json:"collect_interval"`
			StoreHistory      *bool     `json:"store_history"`
			HistoryInterval   int       `json:"history_interval"`
			ReadbackTolerance float64   `json:"readback_tolerance"`
		}
		if e := c.ShouldBindJSON(&p); e != nil {
			response.Error(c, 400, 41001, "参数格式错误", nil)
			return
		}
		id := uuid.Nil
		if update {
			var e error
			id, e = uuid.Parse(c.Param("id"))
			if e != nil {
				response.Error(c, 400, 41001, "无效ID", nil)
				return
			}
		}
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}
		storeHistory := true
		if p.StoreHistory != nil {
			storeHistory = *p.StoreHistory
		}
		if p.CollectInterval == 0 {
			p.CollectInterval = 1
		}
		if p.HistoryInterval == 0 {
			p.HistoryInterval = 1
		}
		if p.ReadbackTolerance == 0 {
			p.ReadbackTolerance = .0001
		}
		row := postgres.PointRow{ID: id, Name: p.Name, GroupName: p.GroupName, DeviceID: p.DeviceID, Enabled: enabled, WriteEnabled: p.WriteEnabled, Address: p.Address, DataType: p.DataType, Unit: p.Unit, CollectInterval: p.CollectInterval, StoreHistory: storeHistory, HistoryInterval: p.HistoryInterval, ReadbackTolerance: p.ReadbackTolerance}
		if e := h.Platform.SavePoint(c, kind, &row); e != nil {
			code := 41001
			if kind == "write" {
				code = 42001
			}
			response.Error(c, 400, code, e.Error(), nil)
			return
		}
		if update {
			response.OK(c, row)
		} else {
			response.Created(c, row)
		}
	}
}
func (h *Handler) deletePoint(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, e := uuid.Parse(c.Param("id"))
		if e != nil {
			response.Error(c, 400, 41001, "无效ID", nil)
			return
		}
		if e = h.Platform.DeletePoint(c, kind, id); e != nil {
			response.Error(c, 404, 41004, "点位不存在", nil)
			return
		}
		response.OK(c, nil)
	}
}
func (h *Handler) writePoint(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		response.Error(c, 400, 42001, "无效ID", nil)
		return
	}
	var req struct {
		Value    any     `json:"value"`
		Reason   *string `json:"reason"`
		Source   any     `json:"source"`
		Operator any     `json:"operator"`
	}
	if e = c.ShouldBindJSON(&req); e != nil || req.Source != nil || req.Operator != nil {
		response.Error(c, 400, 42001, "参数格式错误，source/operator不允许由请求指定", nil)
		return
	}
	data, code, e := h.Platform.ExecuteWrite(c, id, req.Value, req.Reason)
	if e != nil {
		status := http.StatusBadRequest
		if code >= 51000 {
			status = http.StatusBadGateway
		}
		response.Error(c, status, code, e.Error(), data)
		return
	}
	response.OK(c, data)
}
func (h *Handler) logs(c *gin.Context) {
	items, e := h.Platform.ListLogs(c)
	if e != nil {
		serverError(c, e)
		return
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if v := c.Query("point_id"); v != "" && fmt.Sprint(item["point_id"]) != v {
			continue
		}
		if v := c.Query("device_id"); v != "" && fmt.Sprint(item["device_id"]) != v {
			continue
		}
		if v := c.Query("result"); v != "" && fmt.Sprint(item["result"]) != v {
			continue
		}
		if v := strings.ToLower(c.Query("keyword")); v != "" && !strings.Contains(strings.ToLower(fmt.Sprint(item["point_name"])), v) {
			continue
		}
		filtered = append(filtered, item)
	}
	pageNo, pageSize := pagination(c)
	start, end := bounds(len(filtered), pageNo, pageSize)
	response.OK(c, map[string]any{"total": len(filtered), "page": pageNo, "page_size": pageSize, "items": filtered[start:end]})
}
func page(items any) map[string]any {
	return map[string]any{"total": length(items), "page": 1, "page_size": 100, "items": items}
}
func length(v any) int {
	switch x := v.(type) {
	case []model.Device:
		return len(x)
	case []map[string]any:
		return len(x)
	default:
		b, _ := json.Marshal(v)
		var a []any
		json.Unmarshal(b, &a)
		return len(a)
	}
}
func (h *Handler) tree(c *gin.Context) {
	tree, e := h.History.Tree(c)
	if e != nil {
		serverError(c, e)
		return
	}
	response.OK(c, map[string]any{"tree": tree})
}

type historyRequest struct {
	PointIDs        []uuid.UUID `json:"point_ids"`
	StartTime       time.Time   `json:"start_time"`
	EndTime         time.Time   `json:"end_time"`
	MaxSamples      int         `json:"max_samples"`
	IntervalMinutes int         `json:"interval_minutes"`
}

func bindHistory(c *gin.Context) (historyRequest, bool) {
	var r historyRequest
	if e := c.ShouldBindJSON(&r); e != nil {
		response.Error(c, 400, 43001, "参数格式错误", nil)
		return r, false
	}
	if len(r.PointIDs) < 1 || len(r.PointIDs) > 20 {
		response.Error(c, 400, 43002, "点位数量必须为1~20", nil)
		return r, false
	}
	if !r.EndTime.After(r.StartTime) || r.EndTime.Sub(r.StartTime) > 31*24*time.Hour {
		response.Error(c, 400, 43003, "时间范围无效或超过31天", nil)
		return r, false
	}
	return r, true
}
func (h *Handler) historyQuery(c *gin.Context) {
	r, ok := bindHistory(c)
	if !ok {
		return
	}
	if r.MaxSamples == 0 {
		r.MaxSamples = 2000
	}
	if r.MaxSamples < 100 || r.MaxSamples > 10000 {
		response.Error(c, 400, 43001, "max_samples必须为100~10000", nil)
		return
	}
	data, e := h.History.Query(c, r.PointIDs, r.StartTime, r.EndTime, r.MaxSamples)
	if e != nil {
		historyError(c, e)
		return
	}
	response.OK(c, map[string]any{"series": data})
}
func (h *Handler) historyTable(c *gin.Context) {
	r, ok := bindHistory(c)
	if !ok {
		return
	}
	if r.IntervalMinutes < 1 || r.IntervalMinutes > 1440 {
		response.Error(c, 400, 43001, "interval_minutes必须为1~1440", nil)
		return
	}
	data, e := h.History.QueryTable(c, r.PointIDs, r.StartTime, r.EndTime, r.IntervalMinutes)
	if e != nil {
		historyError(c, e)
		return
	}
	response.OK(c, data)
}
func (h *Handler) exportHistory(c *gin.Context) {
	r, ok := bindHistory(c)
	if !ok {
		return
	}
	if r.IntervalMinutes < 1 || r.IntervalMinutes > 1440 {
		response.Error(c, 400, 43001, "interval_minutes必须为1~1440", nil)
		return
	}
	rows := int(r.EndTime.Sub(r.StartTime)/(time.Duration(r.IntervalMinutes)*time.Minute)) + 1
	if rows > 50000 {
		response.Error(c, 413, 43006, "导出行数超过50000", nil)
		return
	}
	data, e := h.History.QueryTable(c, r.PointIDs, r.StartTime, r.EndTime, r.IntervalMinutes)
	if e != nil {
		historyError(c, e)
		return
	}
	name := fmt.Sprintf("history_%s_%s_%dm.csv", r.StartTime.Format("20060102T150405"), r.EndTime.Format("20060102T150405"), r.IntervalMinutes)
	c.Header("Content-Type", "text/csv; charset=utf-8-sig")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(c.Writer)
	head := []string{"时间"}
	used := map[string]bool{}
	for _, col := range data.Columns {
		n := col.PointName
		if col.Unit != nil {
			n += "[" + *col.Unit + "]"
		}
		if used[n] {
			n += "_" + col.PointID.String()[:8]
		}
		used[n] = true
		head = append(head, n, n+"_质量")
	}
	w.Write(head)
	for i, t := range data.TimeColumn {
		row := []string{t.Format("2006-01-02 15:04:05")}
		for _, col := range data.Columns {
			x := col.Data[i]
			if x.Quality == "good" && x.Value != nil {
				row = append(row, strconv.FormatFloat(*x.Value, 'f', -1, 64), "good")
			} else if x.Quality == "bad" {
				row = append(row, "—", "bad")
			} else {
				row = append(row, "—", "—")
			}
		}
		w.Write(row)
	}
	w.Flush()
}
func serverError(c *gin.Context, e error) {
	slog.Error("服务执行失败", "error", e)
	response.Error(c, 500, 10001, "服务内部错误", nil)
}
func historyError(c *gin.Context, e error) {
	slog.Error("历史查询失败", "error", e)
	code, status := 43005, 502
	if strings.Contains(e.Error(), "元数据不存在") {
		code, status = 43004, 404
	}
	response.Error(c, status, code, map[int]string{43004: "点位元数据不存在", 43005: "TDengine查询失败"}[code], nil)
}

var _ context.Context
