package tmux

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// AttachOptions — как подключиться к терминалу.
type AttachOptions struct {
	// IgnoreSize: клиент не меняет размер окна агента, пока подключён кто-то ещё.
	// Нужен полному экрану и узким окнам, чтобы агент не перерисовывался под них.
	IgnoreSize bool
}

// Attachment — `tmux attach-session` в псевдотерминале: что пишет tmux — читать,
// что ввёл человек — писать. Агент от подключения не зависит: Close отключает только клиента tmux.
type Attachment struct {
	cmd  *exec.Cmd
	pty  *os.File
	once sync.Once
}

// Attach подключается к сессии tmux в псевдотерминале размером cols×rows.
func Attach(name string, cols, rows int, o AttachOptions) (*Attachment, error) {
	if !Exists(name) {
		return nil, ErrNotFound
	}
	args := append(base(), "attach-session")
	if o.IgnoreSize {
		args = append(args, "-f", "ignore-size")
	}
	cmd := exec.Command("tmux", append(args, "-t", "="+name)...)
	cmd.Env = env()
	// Псевдотерминал становится управляющим терминалом клиента: ядро само шлёт ему SIGWINCH
	// при смене размера, а Ctrl+C уходит в сессию байтом — клиент держит терминал в raw-режиме.
	f, err := pty.StartWithSize(cmd, winsize(cols, rows))
	if err != nil {
		return nil, err
	}
	return &Attachment{cmd: cmd, pty: f}, nil
}

func winsize(cols, rows int) *pty.Winsize {
	return &pty.Winsize{Cols: uint16(Clamp(cols, 20, 500, 120)), Rows: uint16(Clamp(rows, 5, 200, 32))}
}

// Read — следующая порция вывода; io.EOF, когда клиент tmux завершился: терминал закрыли
// или подключение закрыто через Close.
func (a *Attachment) Read(p []byte) (int, error) {
	n, err := a.pty.Read(p)
	if err != nil && (errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)) {
		err = io.EOF
	}
	return n, err
}

// Write — ввод человека: клавиши и вставка из буфера.
func (a *Attachment) Write(p []byte) (int, error) { return a.pty.Write(p) }

// Resize меняет размер псевдотерминала, и tmux подгоняет под него окно.
func (a *Attachment) Resize(cols, rows int) error { return pty.Setsize(a.pty, winsize(cols, rows)) }

// Close отключается от сессии. Агент продолжает работать в tmux.
func (a *Attachment) Close() error {
	a.once.Do(func() {
		_ = a.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = a.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = a.cmd.Process.Kill()
			<-done
		}
		_ = a.pty.Close()
	})
	return nil
}
