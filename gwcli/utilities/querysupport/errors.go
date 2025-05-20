package querysupport

type ErrBinaryBlobCoward struct{}

var _ error = ErrBinaryBlobCoward{}

func (err ErrBinaryBlobCoward) Error() string {
	return "refused to print binary data to alternate writer"
}
