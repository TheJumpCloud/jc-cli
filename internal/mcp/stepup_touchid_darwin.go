//go:build darwin

package mcp

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo darwin LDFLAGS: -framework LocalAuthentication -framework Foundation

#import <LocalAuthentication/LocalAuthentication.h>
#import <Foundation/Foundation.h>
#include <dispatch/dispatch.h>
#include <string.h>

// jc_touchid_evaluate presents a biometric prompt to the operator and
// blocks until they accept, decline, or the system errors out.
//
// Return codes:
//   0 — operator authenticated successfully
//   1 — operator explicitly declined / cancelled (LAErrorUserCancel,
//       LAErrorUserFallback, LAErrorSystemCancel, LAErrorAppCancel,
//       LAErrorAuthenticationFailed)
//   2 — biometrics unavailable on this device (no Touch ID, no enrolled
//       fingerprints, hardware locked out, etc.)
//   3 — other error
//
// On non-success returns, errBuf is filled with a short localized
// description from the LAError, truncated to errBufLen.
int jc_touchid_evaluate(const char *reason, char *errBuf, int errBufLen) {
    @autoreleasepool {
        LAContext *ctx = [[LAContext alloc] init];
        NSError *canErr = nil;

        if (![ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics
                              error:&canErr]) {
            if (errBuf && errBufLen > 0) {
                const char *msg = canErr
                    ? [[canErr localizedDescription] UTF8String]
                    : "biometrics unavailable";
                strncpy(errBuf, msg, errBufLen - 1);
                errBuf[errBufLen - 1] = '\0';
            }
            return 2;
        }

        NSString *reasonStr = [NSString stringWithUTF8String:reason];
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);
        __block BOOL ok = NO;
        __block NSInteger errCode = 0;
        __block NSString *errMsg = nil;

        [ctx evaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics
            localizedReason:reasonStr
                      reply:^(BOOL success, NSError * _Nullable error) {
            ok = success;
            if (!success && error) {
                errCode = error.code;
                errMsg = [error localizedDescription];
            }
            dispatch_semaphore_signal(sem);
        }];
        dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);

        if (ok) {
            if (errBuf && errBufLen > 0) errBuf[0] = '\0';
            return 0;
        }

        if (errBuf && errBufLen > 0 && errMsg) {
            strncpy(errBuf, [errMsg UTF8String], errBufLen - 1);
            errBuf[errBufLen - 1] = '\0';
        }

        // User-driven declines map to errStepUpDenied; everything else
        // (biometry lockout, hardware failure) is a generic error.
        switch (errCode) {
            case LAErrorUserCancel:
            case LAErrorUserFallback:
            case LAErrorSystemCancel:
            case LAErrorAppCancel:
            case LAErrorAuthenticationFailed:
                return 1;
            case LAErrorBiometryNotAvailable:
            case LAErrorBiometryNotEnrolled:
            case LAErrorBiometryLockout:
                return 2;
            default:
                return 3;
        }
    }
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"
)

// touchIDStepUp authorizes destructive MCP ops via the macOS biometric
// stack. Unlike ttyStepUp, the prompt is rendered by LocalAuthentication
// as a system modal — independent of the MCP transport — so stdio
// clients (Claude Desktop, Claude Code) can challenge the operator
// without owning a terminal.
type touchIDStepUp struct {
	// Serialize prompts so concurrent destructive ops don't stack
	// biometric dialogs on top of each other. The MCP transport may
	// multiplex tool calls, same as ttyStepUp.
	mu sync.Mutex
}

func newTouchIDStepUp() *touchIDStepUp {
	return &touchIDStepUp{}
}

// newTouchIDStepUpIfSupported is the build-tagged constructor consulted
// by newStepUp. On darwin we always return an authenticator; whether the
// device actually has Touch ID enrolled is decided at authorize-time
// (LAContext.canEvaluatePolicy) so we don't have to probe at startup.
func newTouchIDStepUpIfSupported() stepUpAuthenticator {
	return newTouchIDStepUp()
}

func (t *touchIDStepUp) authorize(_ context.Context, toolName, target string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	reason := fmt.Sprintf("Approve MCP %s", toolName)
	if target != "" {
		reason = fmt.Sprintf("Approve MCP %s on %s", toolName, target)
	}

	cReason := C.CString(reason)
	defer C.free(unsafe.Pointer(cReason))

	var buf [256]C.char
	rc := C.jc_touchid_evaluate(cReason, &buf[0], C.int(len(buf)))

	switch rc {
	case 0:
		return nil
	case 1:
		return errStepUpDenied
	case 2:
		return errStepUpUnavailable
	default:
		msg := C.GoString(&buf[0])
		if msg == "" {
			return fmt.Errorf("touch id: unknown error")
		}
		return fmt.Errorf("touch id: %s", msg)
	}
}
