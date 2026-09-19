package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"
)

// Server runs the unix control API plus optional device-facing HTTP/TFTP/SFTP.
type Server struct {
	Repo   *Repo
	Config Config
	Socket string
}

func (s *Server) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	if s.Socket != "" {
		ln, err := listenUnix(s.Socket)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: NewAPIHandler(s.Repo)}
		g.Go(func() error {
			slog.Info("storage unix API started", "socket", s.Socket)
			return serveHTTP(ctx, srv, ln)
		})
	}

	if listen := s.Config.HTTPListen; listen != "" {
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			return fmt.Errorf("storage http listen %s: %w", listen, err)
		}
		srv := &http.Server{Handler: NewDeviceHTTP(s.Repo)}
		g.Go(func() error {
			slog.Info("storage device HTTP started", "listen", listen)
			return serveHTTP(ctx, srv, ln)
		})
	}

	if listen := s.Config.TFTPListen; listen != "" {
		udpAddr, err := net.ResolveUDPAddr("udp", listen)
		if err != nil {
			return fmt.Errorf("storage tftp listen %s: %w", listen, err)
		}
		ln, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			return fmt.Errorf("storage tftp listen %s: %w", listen, err)
		}
		g.Go(func() error {
			slog.Info("storage TFTP started", "listen", listen)
			go func() {
				<-ctx.Done()
				ln.Close()
			}()
			return ServeTFTP(ln, s.Repo)
		})
	}

	if listen := s.Config.SFTPListen; listen != "" {
		if s.Config.SFTPUser == "" || s.Config.SFTPPassword == "" {
			return fmt.Errorf("storage sftp listen is set but user/password is empty")
		}
		hostKey, err := loadOrCreateHostKey(s.Repo.Root())
		if err != nil {
			return fmt.Errorf("storage ssh host key: %w", err)
		}
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			return fmt.Errorf("storage sftp listen %s: %w", listen, err)
		}
		g.Go(func() error {
			slog.Info("storage SFTP started", "listen", listen)
			go func() {
				<-ctx.Done()
				ln.Close()
			}()
			return ServeSFTP(ln, s.Repo, s.Config.SFTPUser, s.Config.SFTPPassword, hostKey)
		})
	}

	return g.Wait()
}

func listenUnix(socketPath string) (net.Listener, error) {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir storage socket dir: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func serveHTTP(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}
