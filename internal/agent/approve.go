package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

// ActionRequest is a gated tool call awaiting the user's approval, delivered to
// the GUI. ID correlates the later ApproveAction call back to this request.
type ActionRequest struct {
	ID        string          `json:"id"`
	ToolName  string          `json:"toolName"`
	ToolInput json.RawMessage `json:"toolInput"`
	Cwd       string          `json:"cwd"`
	SessionID string          `json:"sessionId"`
}

// hookPost is the JSON the fleet-hook helper POSTs to /approve.
type hookPost struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	SessionID string          `json:"session_id"`
	Cwd       string          `json:"cwd"`
}

// ApprovalServer is the loopback HTTP endpoint the fleet-hook helper calls. For
// each POST it registers a pending approval, hands the action to the GUI via
// onAction, blocks until the user decides (or timeout/cancel), then answers the
// still-open request with {approved, reason}.
type ApprovalServer struct {
	coord    *Coordinator
	onAction func(ActionRequest)
	timeout  time.Duration
	ctx      context.Context
	srv      *http.Server
	url      string
}

// NewApprovalServer builds the server. ctx (may be nil) cancels any in-flight
// Await so CancelAgent unblocks a waiting hook. onAction emits to the GUI.
func NewApprovalServer(ctx context.Context, coord *Coordinator, timeout time.Duration, onAction func(ActionRequest)) *ApprovalServer {
	return &ApprovalServer{coord: coord, onAction: onAction, timeout: timeout, ctx: ctx}
}

// Start binds an ephemeral loopback port and serves in the background.
func (s *ApprovalServer) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/approve", s.handleApprove)
	s.srv = &http.Server{Handler: mux}
	s.url = "http://" + ln.Addr().String() + "/approve"
	go s.srv.Serve(ln)
	return nil
}

// URL is the endpoint fleet sets as FLEET_HOOK_URL on the claude process.
func (s *ApprovalServer) URL() string { return s.url }

// Stop shuts the server down. A nil ctx uses a short background context.
func (s *ApprovalServer) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
		defer cancel()
	}
	return s.srv.Shutdown(ctx)
}

func (s *ApprovalServer) handleApprove(w http.ResponseWriter, r *http.Request) {
	var p hookPost
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		writeDecision(w, false, "malformed hook request")
		return
	}
	id := s.coord.Register()
	if s.onAction != nil {
		s.onAction(ActionRequest{ID: id, ToolName: p.ToolName, ToolInput: p.ToolInput, Cwd: p.Cwd, SessionID: p.SessionID})
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = r.Context()
	}
	d := s.coord.Await(ctx, id, s.timeout)
	writeDecision(w, d.Approved, d.Reason)
}

func writeDecision(w http.ResponseWriter, approved bool, reason string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"approved": approved, "reason": reason})
}
