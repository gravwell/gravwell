package scaffoldcreate

// An interact is any item capable of being displayed and interacted with by a user in the scaffoldcreate interactive modal.
type interact interface {
	Key() string
	ViewField() string // Returns the right half (field half) of the interact to be paired with the title.
	//Required() bool // TODO remove this and reference fieldConfig instead
	HasNextLine() bool // Returns if this kind of interact has data it wishes to display on a following line.
	NextLine() string  // Returns the data intended for the next line. If !HasNextLine(), should always return "".
}
