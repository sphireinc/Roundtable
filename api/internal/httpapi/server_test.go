package httpapi

import("net/http";"net/http/httptest";"testing")

func TestHealthRequestID(t *testing.T){ r:=httptest.NewRequest(http.MethodGet,"/api/v1/health",nil); r.Header.Set("X-Request-ID","test-1"); w:=httptest.NewRecorder(); NewServer(Config{Version:"test"}).Handler().ServeHTTP(w,r); if w.Code!=http.StatusOK||w.Header().Get("X-Request-ID")!="test-1"{t.Fatalf("unexpected response: %d %q",w.Code,w.Header().Get("X-Request-ID"))} }
func TestHealthGeneratesRequestID(t *testing.T){ r:=httptest.NewRequest(http.MethodGet,"/api/v1/health",nil); w:=httptest.NewRecorder(); NewServer(Config{}).Handler().ServeHTTP(w,r); if w.Header().Get("X-Request-ID")==""{t.Fatal("request id missing")} }
