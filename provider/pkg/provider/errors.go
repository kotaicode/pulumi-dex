package provider

import (
	"errors"
	"fmt"
)

// ErrNotFound reports that Dex does not have the object an operation targeted.
var ErrNotFound = errors.New("not found in Dex")

// NotFoundError reports that a write targeted an object Dex no longer has.
// Dex signals this with a not_found response flag rather than a gRPC status,
// so callers must check the flag explicitly instead of relying on err == nil.
func NotFoundError(operation, resourceType, resourceID string) error {
	return WrapError(operation, resourceType, resourceID,
		fmt.Errorf("%w; it may have been deleted outside Pulumi, run `pulumi refresh` to reconcile state", ErrNotFound))
}

// WrapError wraps a Dex API error with context to make it more user-friendly.
// This provides better error messages that include the operation, resource type,
// and resource ID for easier debugging.
func WrapError(operation, resourceType, resourceID string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("dex %s %s %q: %w", operation, resourceType, resourceID, err)
}
