package errdefs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errClassifier pairs a constructor (which wraps a cause into a typed error)
// with its matching Is* predicate.
type errClassifier struct {
	name      string
	construct func(error) error
	predicate func(error) bool
}

// classifiers covers the constructor/predicate pairs that follow the standard
// shape: construct(nil) == nil, construct is idempotent, and predicate(value)
// is true. The RegistrationHandlerFailed pair has a two-arg constructor
// (err, URI), so it does not fit this table and is asserted separately below.
var classifiers = []errClassifier{
	{"BuildFailed", BuildFailed, IsBuildFailed},
	{"ContainerNameAlreadyInUse", ContainerNameAlreadyInUse, IsContainerNameAlreadyInUse},
	{"ContainerNotFound", ContainerNotFound, IsContainerNotFound},
	{"ImageNotFound", ImageNotFound, IsImageNotFound},
	{"InsufficientPrivileges", InsufficientPrivileges, IsInsufficientPrivileges},
	{"ContainerRemovalAlreadyInProgress", ContainerRemovalAlreadyInProgress, IsContainerRemovalAlreadyInProgress},
	{"DockerfileCannotBeEmpty", DockerfileCannotBeEmpty, IsDockerfileCannotBeEmpty},
	{"DockerfileIsMissing", DockerfileIsMissing, IsDockerfileIsMissing},
	{"DockerBuildFilesNotFound", DockerBuildFilesNotFound, IsDockerBuildFilesNotFound},
	{"DockerStreamCanceled", DockerStreamCanceled, IsDockerStreamCanceled},
	{"DockerComposeNotSupported", DockerComposeNotSupported, IsDockerComposeNotSupported},
}

func TestConstructorWrapsAndPredicateMatches(t *testing.T) {
	cause := errors.New("boom")

	for _, tt := range classifiers {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := tt.construct(cause)
			require.Error(t, wrapped)

			// The predicate recognizes its own wrapped error.
			assert.True(t, tt.predicate(wrapped), "predicate should match its own wrapped error")

			// The wrapped error preserves the cause via errors.Unwrap / errors.Is.
			assert.Equal(t, cause, errors.Unwrap(wrapped), "Unwrap should return the original cause")
			assert.True(t, errors.Is(wrapped, cause), "errors.Is should find the wrapped cause")

			// Error message is delegated to the cause.
			assert.Equal(t, cause.Error(), wrapped.Error())
		})
	}
}

func TestConstructorNilReturnsNil(t *testing.T) {
	for _, tt := range classifiers {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.construct(nil)
			assert.Nil(t, got, "constructing from a nil cause must return nil")
			// And the predicate must not match a nil error.
			assert.False(t, tt.predicate(got))
			assert.False(t, tt.predicate(nil))
		})
	}
}

func TestConstructorIsIdempotent(t *testing.T) {
	cause := errors.New("boom")

	for _, tt := range classifiers {
		t.Run(tt.name, func(t *testing.T) {
			once := tt.construct(cause)
			twice := tt.construct(once)

			// Wrapping an already-wrapped error returns it unchanged (no double wrap).
			assert.Equal(t, once, twice, "constructor should be idempotent")
			assert.True(t, tt.predicate(twice))
			// Still exactly one layer: unwrap once gets back to the cause.
			assert.Equal(t, cause, errors.Unwrap(twice))
		})
	}
}

func TestPredicateDoesNotMatchPlainOrForeignErrors(t *testing.T) {
	plain := errors.New("plain")

	for _, tt := range classifiers {
		t.Run(tt.name, func(t *testing.T) {
			// A plain error is not classified.
			assert.False(t, tt.predicate(plain))

			// Neither is a different category's wrapped error.
			var other error
			if tt.name == "BuildFailed" {
				other = ImageNotFound(plain)
			} else {
				other = BuildFailed(plain)
			}
			assert.False(t, tt.predicate(other), "predicate must not match a different error type")
		})
	}
}

// The Is* predicates use a direct type assertion (not errors.As), so they do
// NOT see through an outer fmt.Errorf %w wrapping. Lock in that behavior.
func TestPredicateDoesNotUnwrapOuterWrapping(t *testing.T) {
	cause := errors.New("boom")

	for _, tt := range classifiers {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := tt.construct(cause)
			outer := fmt.Errorf("context: %w", wrapped)

			assert.False(t, tt.predicate(outer),
				"predicate uses a direct type assertion and must not unwrap outer errors")
			// errors.As, by contrast, can still find it - sanity check the chain is intact.
			require.True(t, errors.Is(outer, cause))
		})
	}
}

// IsRegistrationHandlerFailed recognizes its own error type and nothing else.
func TestRegistrationHandlerFailed_Classification(t *testing.T) {
	cause := errors.New("handler exploded")
	const uri = "wamp.registration.handler"

	regErr := RegistrationHandlerFailed(cause, uri)
	require.Error(t, regErr)

	// Recognizes its own error type.
	assert.True(t, IsRegistrationHandlerFailed(regErr))

	// Does not match an unrelated error type, nil, or a plain error.
	assert.False(t, IsRegistrationHandlerFailed(ContainerRemovalAlreadyInProgress(cause)))
	assert.False(t, IsRegistrationHandlerFailed(nil))
	assert.False(t, IsRegistrationHandlerFailed(cause))

	// The constructor's de-dup guard makes wrapping idempotent: an
	// already-wrapped error is returned as-is, not double-wrapped.
	assert.Equal(t, regErr, RegistrationHandlerFailed(regErr, uri))
}

func TestRegistrationHandlerFailed_ConstructorBehavior(t *testing.T) {
	cause := errors.New("handler exploded")
	const uri = "wamp.registration.handler"

	regErr := RegistrationHandlerFailed(cause, uri)
	require.Error(t, regErr)

	// Error message is delegated to the cause; URI is carried on the struct.
	assert.Equal(t, cause.Error(), regErr.Error())
	assert.Equal(t, cause, errors.Unwrap(regErr))

	rhf, ok := regErr.(ErrRegistrationHandlerFailed)
	require.True(t, ok)
	assert.Equal(t, uri, rhf.URI)
	assert.Equal(t, cause, rhf.Cause())

	// nil cause short-circuits to nil.
	assert.Nil(t, RegistrationHandlerFailed(nil, uri))
}

// NoActionTransition takes no cause and always produces a recognizable error.
func TestNoActionTransition(t *testing.T) {
	err := NoActionTransition()
	require.Error(t, err)

	assert.True(t, IsNoActionTransition(err))
	assert.Equal(t, "no action", err.Error())
	require.NotNil(t, errors.Unwrap(err))
	assert.Equal(t, "no action", errors.Unwrap(err).Error())

	// Does not classify unrelated errors.
	assert.False(t, IsNoActionTransition(errors.New("other")))
	assert.False(t, IsNoActionTransition(nil))
}

// InProgress, unlike the other constructors, does NOT short-circuit on a nil
// cause: it always wraps. Lock in that distinct behavior.
func TestInProgress(t *testing.T) {
	cause := errors.New("still working")

	err := InProgress(cause)
	require.Error(t, err)
	assert.True(t, IsInProgress(err))
	assert.Equal(t, cause, errors.Unwrap(err))
	assert.Equal(t, cause.Error(), err.Error())

	// Wrapping nil still yields a non-nil ErrInProgress value (no short-circuit).
	wrappedNil := InProgress(nil)
	require.NotNil(t, wrappedNil)
	assert.True(t, IsInProgress(wrappedNil))

	assert.False(t, IsInProgress(errors.New("other")))
	assert.False(t, IsInProgress(nil))
}

// Each typed error exposes Cause() returning the wrapped error, matching Unwrap().
func TestCauseMatchesUnwrap(t *testing.T) {
	cause := errors.New("root")

	type causer interface{ Cause() error }

	cases := []struct {
		name string
		err  error
	}{
		{"BuildFailed", BuildFailed(cause)},
		{"ContainerNameAlreadyInUse", ContainerNameAlreadyInUse(cause)},
		{"ContainerNotFound", ContainerNotFound(cause)},
		{"ImageNotFound", ImageNotFound(cause)},
		{"InsufficientPrivileges", InsufficientPrivileges(cause)},
		{"ContainerRemovalAlreadyInProgress", ContainerRemovalAlreadyInProgress(cause)},
		{"RegistrationHandlerFailed", RegistrationHandlerFailed(cause, "uri")},
		{"DockerfileCannotBeEmpty", DockerfileCannotBeEmpty(cause)},
		{"DockerfileIsMissing", DockerfileIsMissing(cause)},
		{"DockerBuildFilesNotFound", DockerBuildFilesNotFound(cause)},
		{"DockerStreamCanceled", DockerStreamCanceled(cause)},
		{"DockerComposeNotSupported", DockerComposeNotSupported(cause)},
		{"InProgress", InProgress(cause)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := tc.err.(causer)
			require.True(t, ok, "error type should implement Cause()")
			assert.Equal(t, cause, c.Cause())
			assert.Equal(t, errors.Unwrap(tc.err), c.Cause(), "Cause() and Unwrap() should agree")
		})
	}
}

// The package-level sentinels are distinct, stable values usable with errors.Is.
func TestSentinels(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrNotYetImplemented", ErrNotYetImplemented, "not yet implemented"},
		{"ErrNotFound", ErrNotFound, "not found"},
		{"ErrAlreadyExists", ErrAlreadyExists, "already exists"},
		{"ErrFailedToParse", ErrFailedToParse, "failed to parse"},
		{"ErrMissingFromPayload", ErrMissingFromPayload, "missing from payload"},
		{"ErrConfigNotProvided", ErrConfigNotProvided, "no config file provided"},
	}

	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			require.Error(t, s.err)
			assert.Equal(t, s.msg, s.err.Error())

			// errors.Is finds the sentinel through a wrap.
			wrapped := fmt.Errorf("ctx: %w", s.err)
			assert.True(t, errors.Is(wrapped, s.err))
		})
	}

	// Sentinels are mutually distinct.
	all := []error{
		ErrNotYetImplemented, ErrNotFound, ErrAlreadyExists,
		ErrFailedToParse, ErrMissingFromPayload, ErrConfigNotProvided,
	}
	for i := range all {
		for j := range all {
			if i == j {
				continue
			}
			assert.False(t, errors.Is(all[i], all[j]),
				"distinct sentinels must not be errors.Is-equal")
		}
	}
}
