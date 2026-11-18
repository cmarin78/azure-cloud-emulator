package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOperations(t *testing.T) {
	ops := NewOperations()

	idSuccess := ops.Succeeded()
	if idSuccess == "" {
		t.Fatal("expected non-empty operation ID")
	}

	op, ok := ops.Get(idSuccess)
	if !ok || op.Status != "Succeeded" {
		t.Fatalf("expected Succeeded op, got ok=%v op=%+v", ok, op)
	}
	if op.StartTime == "" || op.EndTime == "" {
		t.Errorf("expected non-empty StartTime and EndTime on Succeeded op: %+v", op)
	}

	idFail := ops.Failed("DeploymentFailed", "resource failed to provision")
	opFail, ok := ops.Get(idFail)
	if !ok || opFail.Status != "Failed" {
		t.Fatalf("expected Failed op, got ok=%v op=%+v", ok, opFail)
	}
	if opFail.Error == nil || opFail.Error.Code != "DeploymentFailed" {
		t.Fatalf("expected Error code DeploymentFailed, got %+v", opFail.Error)
	}

	_, ok = ops.Get("non-existent-op")
	if ok {
		t.Fatal("expected false for non-existent op")
	}
}

func TestOperationsHTTPHandler(t *testing.T) {
	ops := NewOperations()
	mux := http.NewServeMux()
	RegisterOperations(mux, ops)

	id := ops.Succeeded()

	// Poll existing op
	url := "/subscriptions/sub-1/providers/Microsoft.Compute/locations/eastus/operationsStatus/" + id
	req := httptest.NewRequest("GET", url, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var status OperationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if status.Status != "Succeeded" {
		t.Fatalf("expected status Succeeded, got %q", status.Status)
	}

	// Poll non-existent op
	req404 := httptest.NewRequest("GET", "/subscriptions/sub-1/providers/Microsoft.Compute/locations/eastus/operationsStatus/op-unknown", nil)
	rec404 := httptest.NewRecorder()
	mux.ServeHTTP(rec404, req404)

	if rec404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown op, got %d", rec404.Code)
	}
}
