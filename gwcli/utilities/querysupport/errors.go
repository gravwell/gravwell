package querysupport

import ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"

// ErrBinaryBlobCoward returns a user-facing error that the given format must be output to a file.
type ErrBinaryBlobCoward string

var _ error = ErrBinaryBlobCoward("format")

func (fmt ErrBinaryBlobCoward) Error() string {
	return "refusing to dump binary blob (format " + string(fmt) + ") to stdout.\n" +
		"If this is intentional, re-run with -" + ft.Output.P() + " <FILENAME>.\n" +
		"If it was not, re-run with --" + ft.CSV.Name + " or --" + ft.JSON.Name + " to download in a more appropriate format."
}

// ErrUnknownSID returns a user-facing error stating that the given sid is unknown.
//
//	querysupport.ErrUnknownSID(sid)
type ErrUnknownSID string

var _ error = ErrUnknownSID("")

func (sid ErrUnknownSID) Error() string {
	return "did not find a search associated to searchID '" + string(sid) + "'"
}
