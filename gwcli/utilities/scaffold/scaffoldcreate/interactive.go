package scaffoldcreate

// An interact is any item capable of being displayed and interacted with by a user in the scaffoldcreate interactive modal.
type interact interface {
	Key() string
	ViewField() string // Returns the right half (field half) of the interact to be paired with the title.
	Required() bool
}
