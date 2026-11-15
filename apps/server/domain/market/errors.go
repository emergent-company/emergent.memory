package market

import (
	"fmt"
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Named instance-side error codes from the install-flow contract.
const (
	CodeIssuerUntrusted       = "market_issuer_untrusted"
	CodeInstallCode           = "install_code_invalid"
	CodeLicenseInvalid        = "license_invalid"
	CodeDigestMismatch        = "digest_mismatch"
	CodeMarketUnreach         = "market_unreachable"
	CodeMarketRateLimited     = "market_rate_limited"
	CodeInstallNotInstallable = "install_not_installable"
	CodeCompat                = "compat_unsatisfied"
	CodeUndeclaredTools       = "undeclared_tools"

	// In-product marketplace contract (register/browse/verify/claim/authorize/
	// usage) codes. They are distinct from the install-flow codes so a caller
	// can tell an install problem from an in-product entitlement or attribution
	// problem.
	CodeListingUntrusted = "listing_untrusted"
	// CodeEntitlementAlreadyHeld is the market's wire code for a repeat claim.
	// The instance treats it as an idempotent success, not an error.
	CodeEntitlementAlreadyHeld = "entitlement_already_held"
	CodeEntitlementClaim       = "entitlement_claim_failed"
	CodeMarketUnauthorized     = "market_unauthorized"
	CodeAuthorizationInvalid   = "authorization_invalid"
	CodeBatchExpired           = "authorization_batch_expired"
	CodeBatchExhausted         = "authorization_batch_exhausted"
	CodeUsageRequestUnknown    = "usage_request_unknown"
	CodeUsageRequestDuplicate  = "usage_request_duplicated"
	CodeUsageReportRejected    = "usage_report_rejected"
	CodeSellerUnreachable      = "seller_unreachable"
	CodeRequestIDMismatch      = "request_id_mismatch"
)

// ErrIssuerUntrusted refuses an install whose market origin is not in the
// operator's allowlist. It is returned BEFORE any outbound request.
func ErrIssuerUntrusted(issuer string) *apperror.Error {
	return apperror.New(http.StatusForbidden, CodeIssuerUntrusted,
		fmt.Sprintf("market issuer %q is not trusted by this instance", issuer))
}

// ErrInstallCodeInvalid covers an unknown, expired, reused or otherwise
// unusable install code.
func ErrInstallCodeInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "install code is invalid or expired"
	}
	return apperror.New(http.StatusBadRequest, CodeInstallCode, detail)
}

// ErrMarketRateLimited reports that the market is throttling installs. It is
// distinct from an invalid code: retrying later may succeed.
func ErrMarketRateLimited(detail string) *apperror.Error {
	if detail == "" {
		detail = "market is rate limiting installs; try again shortly"
	}
	return apperror.New(http.StatusTooManyRequests, CodeMarketRateLimited, detail)
}

// ErrInstallNotInstallable reports that the listing exists but cannot be
// installed here (for example it is withdrawn or not install-delivery). It is
// distinct from an unusable code.
func ErrInstallNotInstallable(detail string) *apperror.Error {
	if detail == "" {
		detail = "listing is not installable on this instance"
	}
	return apperror.New(http.StatusConflict, CodeInstallNotInstallable, detail)
}

// ErrLicenseInvalid covers a license that fails signature, issuer, audience,
// expiry or digest validation.
func ErrLicenseInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "install license is invalid"
	}
	return apperror.New(http.StatusBadRequest, CodeLicenseInvalid, detail)
}

// ErrDigestMismatch reports that downloaded bytes do not match the licensed
// archive digest.
func ErrDigestMismatch(expected, actual string) *apperror.Error {
	msg := "downloaded archive does not match the licensed digest"
	if expected != "" && actual != "" {
		msg = fmt.Sprintf("downloaded archive digest mismatch: expected %s, got %s", expected, actual)
	}
	return apperror.New(http.StatusBadRequest, CodeDigestMismatch, msg)
}

// ErrMarketUnreachable reports a transport or upstream failure talking to the
// market.
func ErrMarketUnreachable(detail string) *apperror.Error {
	if detail == "" {
		detail = "market is unreachable"
	}
	return apperror.New(http.StatusBadGateway, CodeMarketUnreach, detail)
}

// ErrCompatUnsatisfied reports a listing whose compat.memory range does not
// include the running instance version.
func ErrCompatUnsatisfied(constraint, version string) *apperror.Error {
	return apperror.New(http.StatusConflict, CodeCompat,
		fmt.Sprintf("listing requires memory %s; this instance is %s", constraint, version))
}

// ErrUndeclaredTools refuses an install whose blueprint declares tools the
// listing did not licence for install delivery.
func ErrUndeclaredTools(tools []string) *apperror.Error {
	msg := "blueprint declares tools the listing does not licence for install delivery"
	if len(tools) > 0 {
		msg = fmt.Sprintf("%s: %v", msg, tools)
	}
	return apperror.New(http.StatusConflict, CodeUndeclaredTools, msg)
}

// ErrListingUntrusted refuses a listing whose signed trust document is missing,
// malformed, expired, signed by another issuer or names another listing. It is
// returned before the instance offers the listing.
func ErrListingUntrusted(detail string) *apperror.Error {
	if detail == "" {
		detail = "listing trust document is not valid"
	}
	return apperror.New(http.StatusConflict, CodeListingUntrusted, detail)
}

// ErrEntitlementClaim reports that the market declined a claim (unknown or
// withdrawn listing, or an entitlement that cannot be granted for this account).
func ErrEntitlementClaim(detail string) *apperror.Error {
	if detail == "" {
		detail = "market refused the entitlement claim"
	}
	return apperror.New(http.StatusConflict, CodeEntitlementClaim, detail)
}

// ErrMarketUnauthorized reports that the market refused the instance's
// credentials (or that this instance is not registered).
func ErrMarketUnauthorized(detail string) *apperror.Error {
	if detail == "" {
		detail = "market refused this instance's credentials"
	}
	return apperror.New(http.StatusBadGateway, CodeMarketUnauthorized, detail)
}

// ErrAuthorizationInvalid covers an authorization response that is missing its
// assertion, carries the wrong entitlement, or mints an unusable request-id
// batch.
func ErrAuthorizationInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "market returned an invalid call authorization"
	}
	return apperror.New(http.StatusBadRequest, CodeAuthorizationInvalid, detail)
}

// ErrBatchExpired reports that a cached authorization batch can no longer be
// presented to a seller. The caller must wait for the market to issue a new one.
func ErrBatchExpired(entitlementID string) *apperror.Error {
	msg := "authorization batch has expired"
	if entitlementID != "" {
		msg = fmt.Sprintf("authorization batch for entitlement %q has expired", entitlementID)
	}
	return apperror.New(http.StatusConflict, CodeBatchExpired, msg)
}

// ErrBatchExhausted reports that every pre-issued request id in a batch has
// been used. A fresh batch is required.
func ErrBatchExhausted(entitlementID string) *apperror.Error {
	msg := "authorization batch has no request ids left"
	if entitlementID != "" {
		msg = fmt.Sprintf("authorization batch for entitlement %q has no request ids left", entitlementID)
	}
	return apperror.New(http.StatusConflict, CodeBatchExhausted, msg)
}

// ErrUsageRequestUnknown refuses a usage report whose request_id the instance
// never received from the market. It is the instance-side half of §8's
// "fabricated usage reports" control; the market refuses the same id again.
func ErrUsageRequestUnknown(requestID string) *apperror.Error {
	msg := "usage report carries a request_id this instance never received"
	if requestID != "" {
		msg = fmt.Sprintf("usage report request_id %q was never issued to this instance", requestID)
	}
	return apperror.New(http.StatusBadRequest, CodeUsageRequestUnknown, msg)
}

// ErrUsageRequestDuplicated refuses a second usage report for the same
// request_id.
func ErrUsageRequestDuplicated(requestID string) *apperror.Error {
	msg := "usage report has already been sent"
	if requestID != "" {
		msg = fmt.Sprintf("usage report request_id %q has already been sent", requestID)
	}
	return apperror.New(http.StatusConflict, CodeUsageRequestDuplicate, msg)
}

// ErrUsageReportRejected reports that the market refused a usage report even
// though the instance believed the request_id was pre-issued. This is an
// attribution failure worth surfacing, not a silently dropped report.
func ErrUsageReportRejected(detail string) *apperror.Error {
	if detail == "" {
		detail = "market rejected the usage report"
	}
	return apperror.New(http.StatusUnprocessableEntity, CodeUsageReportRejected, detail)
}

// ErrSellerUnreachable reports that the seller's hosted MCP endpoint could not
// be reached.
func ErrSellerUnreachable(detail string) *apperror.Error {
	if detail == "" {
		detail = "seller endpoint is unreachable"
	}
	return apperror.New(http.StatusBadGateway, CodeSellerUnreachable, detail)
}

// ErrRequestIDMismatch reports that a seller did not echo the request_id the
// instance presented. Without the echo the call cannot be attributed.
func ErrRequestIDMismatch(detail string) *apperror.Error {
	if detail == "" {
		detail = "seller did not echo the presented request_id"
	}
	return apperror.New(http.StatusBadGateway, CodeRequestIDMismatch, detail)
}

// ErrorCode returns the apperror code carried by err, or "".
func ErrorCode(err error) string {
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr.Code
	}
	return ""
}
