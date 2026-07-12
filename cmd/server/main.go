package main

import (
	"aquacontrolai/internal/api"
	collectorengine "aquacontrolai/internal/engine/collector"
	writerengine "aquacontrolai/internal/engine/writer"
	"aquacontrolai/internal/pkg/config"
	"aquacontrolai/internal/protocol"
	"aquacontrolai/internal/protocol/modbus"
	"aquacontrolai/internal/protocol/s7"
	pg "aquacontrolai/internal/repository/postgres"
	td "aquacontrolai/internal/repository/tdengine"
	"aquacontrolai/internal/service/platform"
	"aquacontrolai/migrations"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/taosdata/driver-go/v3/taosRestful"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, e := pgxpool.New(ctx, cfg.PostgresDSN)
	must(e)
	mustPing(ctx, db)
	_, e = db.Exec(ctx, migrations.PostgreSQL)
	must(e)
	_, e = db.Exec(ctx, migrations.PostgreSQLGroups)
	must(e)
	db.Close()
	pgStore, e := pg.Open(ctx, cfg.PostgresDSN)
	must(e)
	defer pgStore.DB.Close()
	adminDSN := strings.TrimSuffix(cfg.TDengineDSN, "/"+cfg.TDengineDatabase) + "/"
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`).MatchString(cfg.TDengineDatabase) {
		must(errors.New("TDENGINE_DATABASE 名称无效"))
	}
	admin, e := sql.Open("taosRestful", adminDSN)
	must(e)
	_, e = admin.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+cfg.TDengineDatabase+"` KEEP "+fmt.Sprint(cfg.HistoryRetentionDays))
	must(e)
	admin.Close()
	taos, e := td.Open(ctx, cfg.TDengineDSN, cfg.TDengineDatabase)
	must(e)
	defer taos.DB.Close()
	for _, statement := range strings.Split(migrations.TDengine, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		_, e = taos.DB.ExecContext(ctx, statement)
		must(e)
	}
	registry := protocol.NewRegistry()
	must(registry.Register(s7.Factory{}))
	must(registry.Register(modbus.Factory{}))
	connections := collectorengine.NewManager(pgStore, registry)
	defer connections.Close()
	collector := collectorengine.NewEngine(connections, pgStore, taos, cfg.CollectorWorkers)
	collector.Start(ctx)
	defer collector.Stop()
	writeEngine := &writerengine.Engine{Manager: connections, Store: pgStore}
	svc := &platform.Service{Store: pgStore, Registry: registry, Connections: connections, Writer: writeEngine, Collector: collector, TD: taos}
	router := api.NewRouter(&api.Handler{Platform: svc, History: &platform.History{PG: pgStore, TD: taos, Collector: collector}})
	server := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("AquaControl AI 服务启动", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务异常", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	must(server.Shutdown(shutdown))
}
func must(e error) {
	if e != nil {
		slog.Error("应用启动失败", "error", e)
		os.Exit(1)
	}
}
func mustPing(ctx context.Context, db *pgxpool.Pool) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	must(db.Ping(c))
}

var _ *sql.DB
