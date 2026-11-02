package sessionjsonl

import "errors"

// ErrInvalidHeader reports a header or record that cannot be parsed.
//
// The sentinels are exported so a caller can classify a failure with errors.Is
// instead of matching on a message. A storage problem a caller cannot identify
// programmatically forces it to parse strings, which is exactly the coupling the
// error values exist to remove.
var ErrInvalidHeader = errors.New("sessionjsonl: invalid header")

// ErrForeignFormat reports an artifact written by a different implementation.
//
// It is distinct from ErrUnsupportedVersion because the two need different
// responses: a foreign file must not be touched at all, while a newer file of
// our own is a build to upgrade.
var ErrForeignFormat = errors.New("sessionjsonl: not a dsh-go session artifact")

// ErrUnsupportedVersion reports an artifact whose version this build does not
// understand. It is refused rather than migrated.
var ErrUnsupportedVersion = errors.New("sessionjsonl: unsupported format version")

// ErrCorruptRecord reports a record whose bytes are damaged.
var ErrCorruptRecord = errors.New("sessionjsonl: corrupt record")

// ErrTornTail reports an incomplete final record.
//
// It is separate from ErrCorruptRecord because the two are handled differently:
// a torn tail is dropped by recovery, while damage in the middle is refused.
var ErrTornTail = errors.New("sessionjsonl: incomplete final record")

// ErrInvalidValue reports a provider option that cannot be used.
var ErrInvalidValue = errors.New("sessionjsonl: invalid value")

// ErrDataDirMissing reports a provider configured without a data directory.
var ErrDataDirMissing = errors.New("sessionjsonl: data directory is required")
