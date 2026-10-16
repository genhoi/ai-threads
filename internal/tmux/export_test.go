package tmux

// SetServerScope подменяет начало команды, которая запускает сервер tmux; возвращает отмену.
func SetServerScope(prefix []string) (restore func()) {
	old := serverScope
	serverScope = func() []string { return prefix }
	return func() { serverScope = old }
}
