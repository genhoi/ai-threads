package server

import (
	"net/http"

	"github.com/genhoi/ai-threads/internal/remote"
)

// --- Доступ с телефона ---

// localOnly — запрос только с компьютера: с телефона нельзя ни увидеть ключ входа,
// ни поменять, кто и как подключается.
func (s *Server) localOnly(fn func(w http.ResponseWriter, r *http.Request) error) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		if remote.FromRemote(r) {
			return apiErr(http.StatusForbidden, "local_only")
		}
		return fn(w, r)
	}
}

// syncRemote поднимает, останавливает или перезапускает узел по настройкам.
func (s *Server) syncRemote() {
	r := s.settings.Get().Remote
	s.remote.Apply(remote.Config{Enabled: r.Enabled, Funnel: r.Funnel, Hostname: r.Hostname, Dir: r.RemoteDir()})
}

// remoteView — состояние доступа с телефона со ссылкой входа. Отдаётся только на компьютер.
func (s *Server) remoteView(lang string) RemoteView {
	set := s.settings.Get().Remote
	st := s.remote.Status()
	v := RemoteView{Enabled: set.Enabled, Funnel: set.Funnel, State: string(st.State), Since: unix(st.Since),
		LoginURL: st.LoginURL, Host: st.Host, Devices: []DeviceView{}}
	if st.Host != "" {
		v.URL = "https://" + st.Host + "/"
		if key := s.remote.Keys().Key(); key != "" {
			v.Link = v.URL + "?key=" + key
		}
	}
	if st.Err != "" {
		v.Error = st.Err
		if st.ErrCode != "" {
			v.Error = apiErr(0, "remote_"+st.ErrCode).text(lang)
		}
	}
	for _, d := range s.remote.Keys().Devices() {
		v.Devices = append(v.Devices, DeviceView{ID: d.ID, Name: d.Name, Created: d.Created, LastSeen: d.LastSeen})
	}
	return v
}

// remoteBrief — состояние и адрес для первой отрисовки, без ключа.
func (s *Server) remoteBrief() RemoteBrief {
	st := s.remote.Status()
	return RemoteBrief{State: string(st.State), Host: st.Host}
}

func (s *Server) handleRemote(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, s.remoteView(s.lang(r)))
	return nil
}

// handleSetRemote включает и выключает доступ и Funnel. Настройка сохраняется в settings.json,
// узел поднимается в фоне: ход виден в состоянии и в событиях remote.
func (s *Server) handleSetRemote(w http.ResponseWriter, r *http.Request) error {
	var req RemoteRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	cur := s.settings.Get()
	if req.Enabled != nil {
		cur.Remote.Enabled = *req.Enabled
	}
	if req.Funnel != nil {
		cur.Remote.Funnel = *req.Funnel
	}
	if err := s.settings.Save(cur); err != nil {
		return err
	}
	s.syncRemote()
	s.events.touchRemote()
	if cur.Remote.Enabled {
		// Включили снова после ошибки — пробовать сразу, а не ждать паузы.
		s.remote.Retry()
	}
	writeJSON(w, http.StatusOK, s.remoteView(s.lang(r)))
	return nil
}

// handleRemoteKey — «Новый ключ»: старые ссылки перестают работать, все телефоны отключаются.
func (s *Server) handleRemoteKey(w http.ResponseWriter, r *http.Request) error {
	if err := s.remote.NewKey(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.remoteView(s.lang(r)))
	return nil
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) error {
	ok, err := s.remote.Revoke(r.PathValue("id"))
	if err != nil {
		return err
	}
	if !ok {
		return apiErr(http.StatusNotFound, "no_device")
	}
	writeJSON(w, http.StatusOK, s.remoteView(s.lang(r)))
	return nil
}

// handleRemoteQR — QR-код ссылки входа. Ссылки нет, пока узел не получил адрес.
func (s *Server) handleRemoteQR(w http.ResponseWriter, r *http.Request) error {
	link := s.remoteView(s.lang(r)).Link
	if link == "" {
		return apiErr(http.StatusConflict, "no_link")
	}
	svg, err := remote.QRSVG(link)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(svg)
	return nil
}
