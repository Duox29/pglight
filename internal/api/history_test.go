package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pglight/internal/store"
)

func TestHistoryRecordsFailuresAndSupportsFilteredPinnedDeletion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsureUser(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Store: s, UserID: "u"}
	post := httptest.NewRecorder()
	h.History(post, httptest.NewRequest("POST", "/api/history", strings.NewReader(`{"session_id":"s1","sql":"select private_col from x","ms":18,"success":false,"error_code":"42501","error_message":"permission denied"}`)))
	if post.Code != 200 {
		t.Fatalf("post status=%d body=%s", post.Code, post.Body)
	}
	var postBody map[string]any
	if err := json.Unmarshal(post.Body.Bytes(), &postBody); err != nil {
		t.Fatal(err)
	}
	id, _ := postBody["id"].(string)
	if id == "" {
		t.Fatal("POST history should return its entry id")
	}
	pinReq := httptest.NewRequest("PUT", "/api/history", strings.NewReader(`{"id":"`+id+`","pinned":true}`))
	pinRec := httptest.NewRecorder()
	h.History(pinRec, pinReq)
	if pinRec.Code != 200 {
		t.Fatalf("pin status=%d body=%s", pinRec.Code, pinRec.Body)
	}
	list := httptest.NewRecorder()
	h.History(list, httptest.NewRequest("GET", "/api/history?q=private_col&connection_id=s1&status=failed&pinned=true", nil))
	var result struct {
		History []map[string]any `json:"history"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if list.Code != 200 || len(result.History) != 1 || result.History[0]["error_code"] != "42501" || result.History[0]["pinned"] != true {
		t.Fatalf("status=%d result=%+v", list.Code, result)
	}
	del := httptest.NewRecorder()
	h.History(del, httptest.NewRequest("DELETE", "/api/history?id="+id, nil))
	if del.Code != 200 {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body)
	}
}
