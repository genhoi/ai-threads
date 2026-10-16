// Команда nit запускает «Нить»: локальный сервер, через который запускают агентов
// для программирования и работают с ними.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/genhoi/ai-threads/internal/remote"
	"github.com/genhoi/ai-threads/internal/server"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/store"
)

// version задаётся при сборке: -ldflags "-X main.version=…".
var version = "dev"

func main() {
	port := flag.Int("port", 8765, "порт на 127.0.0.1")
	gateToken := flag.String("import-gate-token", "",
		"взять ключ входа с телефона из файла ключа gate (gate/state/token.txt), чтобы телефоны, вошедшие через gate, остались подключены; "+
			"то же — переменная NIT_IMPORT_GATE_TOKEN")
	flag.Parse()

	conf := settings.New(settings.Path())
	if err := conf.Err(); err != nil {
		// Неверные поля заменены умолчаниями: «Нить» работает, а правка видна на экране настроек.
		log.Printf("settings: %v", err)
	}
	marks, err := store.Open(store.DefaultPath())
	if err != nil {
		// Повреждённый state.json не затирается: человек должен его увидеть.
		log.Fatal(err)
	}

	// Только 127.0.0.1: через «Нить» можно выполнять команды на компьютере.
	// Порт занимается до чтения журналов: при занятом порте читать их незачем.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	data := store.DataDir()
	// Ключ входа с телефона и устройства — отдельно от пометок, с правами 0600.
	keys, err := remote.Open(filepath.Join(data, "remote.json"))
	if err != nil {
		log.Fatal(err)
	}
	importGateToken(keys, *gateToken)
	app := server.New(server.Options{
		Port:       addr.Port,
		Version:    version,
		Settings:   conf,
		Store:      marks,
		CacheFile:  filepath.Join(data, "catalog.json"),
		StateFile:  filepath.Join(data, "terminals.json"),
		RemoteKeys: keys,
	})
	srv := &http.Server{Handler: app, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	running := make(chan struct{})
	go func() {
		defer close(running)
		app.Run(ctx)
	}()
	go func() {
		<-ctx.Done()
		// Второй Ctrl+C или SIGTERM завершает процесс сразу, не дожидаясь остановки.
		stop()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	fmt.Printf("Нить: http://127.0.0.1:%d\n", addr.Port)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	// Run пишет кэш каталога и состояния терминалов и закрывает узел Tailscale перед выходом.
	<-running
}

// importGateToken берёт ключ входа из файла gate: флаг важнее переменной окружения.
// Сам ключ в журнал не пишется. Тот же файл второй раз ключ не меняет, поэтому переменную
// можно оставить в службе: «Новый ключ» в настройках она не отменит.
func importGateToken(keys *remote.Keys, flagPath string) {
	path, fatal := flagPath, true
	if path == "" {
		path, fatal = os.Getenv("NIT_IMPORT_GATE_TOKEN"), false
	}
	if path == "" {
		return
	}
	changed, err := keys.ImportGateToken(path)
	switch {
	case err != nil && fatal:
		log.Fatalf("import-gate-token: %v", err)
	case err != nil:
		log.Printf("NIT_IMPORT_GATE_TOKEN: %v", err)
	case changed:
		log.Printf("remote: ключ входа взят из %s", path)
	}
}
