package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

type actorContextKey struct{}

type actorIdentity struct {
	ID   string
	Kind string
	Role string
}

const (
	roleView       = "view"
	roleOperate    = "operate"
	roleApprove    = "approve"
	roleGovern     = "govern"
	roleAdmin      = "administer"
	roleForce      = "force-override"
	roleHuman      = "human" // legacy alias accepted in local developer mode
	roleAgent      = "agent"
	actorHuman     = "human"
	actorAgent     = "agent"
	agentTokenHead = "Bearer "
)

type securityCapabilitiesResponse struct {
	Authentication string   `json:"authentication"`
	AgentBoundary  string   `json:"agent_boundary"`
	CSRF           string   `json:"csrf"`
	DefaultBind    string   `json:"default_bind"`
	Roles          []string `json:"roles"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

func (s *Server) securityCapabilitiesAPI(w http.ResponseWriter, r *http.Request) {
	authentication := "local-header-development"
	if s.config.HumanToken != "" || s.config.AgentToken != "" {
		authentication = "bearer-token"
	}
	writeJSON(w, http.StatusOK, securityCapabilitiesResponse{
		Authentication: authentication,
		AgentBoundary:  "agents require a separate agent bearer token when configured; browser actor headers are never sufficient remotely",
		CSRF:           "state-changing requests with an Origin header must match an allowed local UI origin",
		DefaultBind:    "127.0.0.1",
		Roles:          []string{roleView, roleOperate, roleApprove, roleGovern, roleAdmin, roleForce},
		AllowedOrigins: append([]string(nil), s.config.AllowedOrigins...),
	})
}

func identityFromRequest(r *http.Request) actorIdentity {
	if identity, ok := r.Context().Value(actorContextKey{}).(actorIdentity); ok {
		return identity
	}
	role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Actor-Role")))
	kind := actorHuman
	if role == roleAgent || r.Header.Get("X-Roundtable-Orchestrator") == "true" {
		kind = actorAgent
	}
	return actorIdentity{ID: strings.TrimSpace(r.Header.Get("X-Actor-ID")), Kind: kind, Role: role}
}

func withActor(r *http.Request, identity actorIdentity) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorContextKey{}, identity))
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(value, agentTokenHead) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, agentTokenHead))
}

func tokenMatches(got, expected string) bool {
	if got == "" || expected == "" || len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func humanRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case roleHuman, "admin", "chair", roleView, roleOperate, roleApprove, roleGovern, roleAdmin, roleForce:
		return true
	default:
		return false
	}
}

func agentIdentity(r *http.Request) bool {
	return identityFromRequest(r).Kind == actorAgent
}

func withSecurityPolicy(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		identity := actorIdentity{ID: strings.TrimSpace(r.Header.Get("X-Actor-ID")), Role: strings.ToLower(strings.TrimSpace(r.Header.Get("X-Actor-Role"))), Kind: actorHuman}
		if token := bearerToken(r); token != "" {
			switch {
			case tokenMatches(token, s.config.HumanToken):
				identity.Kind = actorHuman
			case tokenMatches(token, s.config.AgentToken):
				identity.Kind, identity.Role = actorAgent, roleAgent
			default:
				WriteProblem(w, r, http.StatusUnauthorized, "invalid_token", "Authentication failed", "The bearer token is not valid for this control surface")
				return
			}
		} else if s.config.HumanToken != "" || s.config.AgentToken != "" {
			WriteProblem(w, r, http.StatusUnauthorized, "authentication_required", "Authentication required", "Use a configured bearer token")
			return
		} else if identity.Role == roleAgent || r.Header.Get("X-Roundtable-Orchestrator") == "true" {
			identity.Kind = actorAgent
		}
		if identity.Kind == actorHuman && identity.Role != "" && !humanRole(identity.Role) {
			WriteProblem(w, r, http.StatusForbidden, "unknown_role", "Unknown actor role", "Use view, operate, approve, govern, administer, or force-override")
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !s.originAllowed(origin) {
				WriteProblem(w, r, http.StatusForbidden, "csrf_origin_forbidden", "Cross-site request rejected", "State-changing browser requests must originate from an allowed local UI origin")
				return
			}
		}
		next.ServeHTTP(w, withActor(r, identity))
	})
}
