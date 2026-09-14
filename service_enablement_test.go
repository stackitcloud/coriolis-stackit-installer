package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v1api"
)

func TestEnsureRunCommandServiceAlreadyEnabled(t *testing.T) {
	enableCalls := 0
	get := func(serviceenablement.ApiGetServiceStatusRequest) (*serviceenablement.ServiceStatus, error) {
		return runCommandStatus(serviceenablement.SERVICESTATUSSTATE_ENABLED), nil
	}
	enable := func(serviceenablement.ApiEnableServiceRequest) error {
		enableCalls++
		return nil
	}
	c := &Cloud{enablement: serviceenablement.DefaultAPIServiceMock{GetServiceStatusExecuteMock: &get, EnableServiceExecuteMock: &enable}, poll: time.Millisecond}
	if err := c.ensureRunCommandService(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	if enableCalls != 0 {
		t.Fatalf("enable calls=%d, want 0", enableCalls)
	}
}

func TestEnsureRunCommandServiceEnablesAndWaits(t *testing.T) {
	states := []serviceenablement.ServiceStatusState{
		serviceenablement.SERVICESTATUSSTATE_DISABLED,
		serviceenablement.SERVICESTATUSSTATE_ENABLING,
		serviceenablement.SERVICESTATUSSTATE_ENABLED,
	}
	getCalls := 0
	get := func(serviceenablement.ApiGetServiceStatusRequest) (*serviceenablement.ServiceStatus, error) {
		state := states[min(getCalls, len(states)-1)]
		getCalls++
		return runCommandStatus(state), nil
	}
	enableCalls := 0
	enable := func(serviceenablement.ApiEnableServiceRequest) error {
		enableCalls++
		return nil
	}
	c := &Cloud{enablement: serviceenablement.DefaultAPIServiceMock{GetServiceStatusExecuteMock: &get, EnableServiceExecuteMock: &enable}, poll: time.Millisecond}
	if err := c.ensureRunCommandService(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	if enableCalls != 1 {
		t.Fatalf("enable calls=%d, want 1", enableCalls)
	}
	if getCalls != 3 {
		t.Fatalf("status calls=%d, want 3", getCalls)
	}
}

func TestEnsureRunCommandServiceExplainsPermission(t *testing.T) {
	get := func(serviceenablement.ApiGetServiceStatusRequest) (*serviceenablement.ServiceStatus, error) {
		return runCommandStatus(serviceenablement.SERVICESTATUSSTATE_DISABLED), nil
	}
	enable := func(serviceenablement.ApiEnableServiceRequest) error { return errors.New("403 Forbidden") }
	c := &Cloud{enablement: serviceenablement.DefaultAPIServiceMock{GetServiceStatusExecuteMock: &get, EnableServiceExecuteMock: &enable}, poll: time.Millisecond}
	err := c.ensureRunCommandService(context.Background(), "project")
	if err == nil || !strings.Contains(err.Error(), "Project Editor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runCommandStatus(state serviceenablement.ServiceStatusState) *serviceenablement.ServiceStatus {
	status := serviceenablement.NewServiceStatus()
	status.SetState(state)
	return status
}
