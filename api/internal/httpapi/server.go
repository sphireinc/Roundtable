package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Config struct { Version string; Logger *slog.Logger }
type Server struct{ config Config }
type problem struct { Type string `json:"type"`; Title string `json:"title"`; Status int `json:"status"`; Detail string `json:"detail,omitempty"`; Instance string `json:"instance,omitempty"`; RequestID string `json:"request_id"`; Code string `json:"code,omitempty"` }

func NewServer(cfg Config) *Server { if cfg.Version == "" { cfg.Version = "dev" }; if cfg.Logger == nil { cfg.Logger = slog.Default() }; return &Server{config: cfg} }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/status", s.status)
	return requestIDs(securityHeaders(jsonDefaults(mux)))
}
func (s *Server) Serve(ctx context.Context, addr string) error { if strings.TrimSpace(addr)=="" { addr="127.0.0.1:8080" }; srv:=&http.Server{Addr:addr,Handler:s.Handler(),ReadHeaderTimeout:5*time.Second}; go func(){ <-ctx.Done(); shutdown,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel(); _=srv.Shutdown(shutdown) }(); err:=srv.ListenAndServe(); if err==http.ErrServerClosed{return nil}; return err }
func (s *Server) health(w http.ResponseWriter,r *http.Request){ writeJSON(w,http.StatusOK,map[string]string{"status":"ok","version":s.config.Version,"request_id":requestID(r.Context())}) }
func (s *Server) status(w http.ResponseWriter,r *http.Request){ writeJSON(w,http.StatusOK,map[string]string{"status":"ok","version":s.config.Version,"api_version":"v1","time":time.Now().UTC().Format(time.RFC3339Nano),"request_id":requestID(r.Context())}) }
type requestIDKey struct{}
func requestID(ctx context.Context) string { if v,ok:=ctx.Value(requestIDKey{}).(string);ok{return v}; return "unknown" }
func newRequestID() string { b:=make([]byte,16); if _,err:=rand.Read(b);err!=nil{return fmt.Sprintf("req-%d",time.Now().UnixNano())}; return "req-"+hex.EncodeToString(b) }
func requestIDs(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ id:=strings.TrimSpace(r.Header.Get("X-Request-ID")); if id==""||len(id)>128||strings.ContainsAny(id,"\r\n"){id=newRequestID()}; w.Header().Set("X-Request-ID",id); next.ServeHTTP(w,r.WithContext(context.WithValue(r.Context(),requestIDKey{},id))) }) }
func securityHeaders(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ w.Header().Set("X-Content-Type-Options","nosniff"); w.Header().Set("Cache-Control","no-store"); next.ServeHTTP(w,r) }) }
func jsonDefaults(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ w.Header().Set("Content-Type","application/json; charset=utf-8"); next.ServeHTTP(w,r) }) }
func writeJSON(w http.ResponseWriter,status int,v any){ w.WriteHeader(status); _=json.NewEncoder(w).Encode(v) }
func WriteProblem(w http.ResponseWriter,r *http.Request,status int,code,title,detail string){ w.Header().Set("Content-Type","application/problem+json; charset=utf-8"); writeJSON(w,status,problem{Type:"https://roundtable.dev/problems/"+code,Title:title,Status:status,Detail:detail,Instance:r.URL.Path,RequestID:requestID(r.Context()),Code:code}) }
