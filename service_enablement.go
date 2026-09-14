package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v1api"
)

const runCommandServiceID = "cloud.stackit.run-command"

func (c *Cloud) runCommandServiceState(ctx context.Context, projectID string) (serviceenablement.ServiceStatusState, error) {
	status, err := c.enablement.GetServiceStatus(ctx, projectID, runCommandServiceID).Execute()
	if err != nil {
		var apiErr *oapierror.GenericOpenAPIError
		if errors.As(err, &apiErr) && apiErr.GetStatusCode() == 404 {
			return serviceenablement.SERVICESTATUSSTATE_DISABLED, nil
		}
		return serviceenablement.SERVICESTATUSSTATE_UNKNOWN_DEFAULT_OPEN_API, err
	}
	if status == nil || !status.HasState() {
		return serviceenablement.SERVICESTATUSSTATE_UNKNOWN_DEFAULT_OPEN_API, errors.New("service enablement API returned no Run Command state")
	}
	return status.GetState(), nil
}

func (c *Cloud) requireRunCommandService(ctx context.Context, projectID string) error {
	state, err := c.runCommandServiceState(ctx, projectID)
	if err != nil {
		return fmt.Errorf("check STACKIT Run Command service: %w", err)
	}
	if state != serviceenablement.SERVICESTATUSSTATE_ENABLED {
		return fmt.Errorf("STACKIT Run Command service is %s in project %s; enable agent.enable_service or activate the service beforehand", state, projectID)
	}
	return nil
}

func (c *Cloud) ensureRunCommandService(ctx context.Context, projectID string) error {
	state, err := c.runCommandServiceState(ctx, projectID)
	if err != nil {
		return fmt.Errorf("check STACKIT Run Command service: %w", err)
	}
	if state == serviceenablement.SERVICESTATUSSTATE_ENABLED {
		return nil
	}

	for state == serviceenablement.SERVICESTATUSSTATE_DISABLING {
		if err := waitForPoll(ctx, c.poll); err != nil {
			return fmt.Errorf("wait for STACKIT Run Command service to finish disabling: %w", err)
		}
		state, err = c.runCommandServiceState(ctx, projectID)
		if err != nil {
			return fmt.Errorf("check STACKIT Run Command service while disabling: %w", err)
		}
	}

	if state == serviceenablement.SERVICESTATUSSTATE_DISABLED {
		fmt.Fprintln(os.Stderr, "enabling STACKIT Run Command service in project", projectID)
		if err := c.enablement.EnableService(ctx, projectID, runCommandServiceID).Execute(); err != nil {
			// Another installer may have won the race. Accept the error only when the
			// service did not transition to an active state.
			current, statusErr := c.runCommandServiceState(ctx, projectID)
			if statusErr != nil || (current != serviceenablement.SERVICESTATUSSTATE_ENABLED && current != serviceenablement.SERVICESTATUSSTATE_ENABLING) {
				return fmt.Errorf("enable STACKIT Run Command service (service account requires Project Editor role): %w", err)
			}
			state = current
		} else {
			state = serviceenablement.SERVICESTATUSSTATE_ENABLING
		}
	}

	if state != serviceenablement.SERVICESTATUSSTATE_ENABLING && state != serviceenablement.SERVICESTATUSSTATE_ENABLED {
		return fmt.Errorf("STACKIT Run Command service entered unsupported state %s", state)
	}
	for state != serviceenablement.SERVICESTATUSSTATE_ENABLED {
		if err := waitForPoll(ctx, c.poll); err != nil {
			return fmt.Errorf("wait for STACKIT Run Command service activation: %w", err)
		}
		state, err = c.runCommandServiceState(ctx, projectID)
		if err != nil {
			if transientAPIError(err) {
				continue
			}
			return fmt.Errorf("check STACKIT Run Command service activation: %w", err)
		}
		if state == serviceenablement.SERVICESTATUSSTATE_DISABLING || state == serviceenablement.SERVICESTATUSSTATE_UNKNOWN_DEFAULT_OPEN_API {
			return fmt.Errorf("STACKIT Run Command service entered unexpected state %s during activation", state)
		}
	}
	fmt.Fprintln(os.Stderr, "STACKIT Run Command service is enabled")
	return nil
}

func waitForPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
