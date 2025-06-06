package credprompt

// NOTE: this testing package relies on teatest, which is an experimental package at the time of authorship (~June 2025).
//
// NOTE 2: as this relies on teatest, you will need a "golden" file, which can be generated via go test -v ./... -update.
// A golden file provides the output/View of the program for automated testing purposes.
// See [this](https://charm.sh/blog/teatest/) blog post for more information.

// NOTE: This test does not work because bubbletea is unable to open a tty on the mocked stdin port.
// The logic is sound, but bubbletea is not compatible with it, hence why the other tests rely on teatest
// I am leaving it as relic code to showcase to fact.
/*func TestManualCredPrompt(t *testing.T) {
	//#region capture stdin so we can send data into it

	// create a pipe to use instead
	_, writeMockSTDIN, err := os.Pipe()
	if err != nil {
		t.Fatal("failed to create stdin pipes:", err)
	}
	origSTDIN := os.Stdin
	os.Stdin = writeMockSTDIN
	t.Cleanup(func() { os.Stdin = origSTDIN })

	//#endregion

	// capture stdout so we can get outputs
	// TODO

	// create a pipe to pull username, password, and error
	results := make(chan struct {
		username string
		password string
		err      error
	})

	t.Run("basic", func(t *testing.T) {
		// spin out a goro to wait on Collect
		go func() {
			u, p, err := Collect("")
			results <- struct {
				username string
				password string
				err      error
			}{u, p, err}
			close(results)
		}()

		// give collect a few moments to spin up
		time.Sleep(time.Second)

		// send username into Collect
		if _, err := writeMockSTDIN.Write([]byte("somename")); err != nil {
			t.Fatal()
		}
		// switch to password
		if _, err := writeMockSTDIN.Write([]byte("\n")); err != nil {
			t.Fatal()
		}
		// send username into Collect
		if _, err := writeMockSTDIN.Write([]byte("somepass")); err != nil {
			t.Fatal()
		}
		// push
		if _, err := writeMockSTDIN.Write([]byte("\n")); err != nil {
			t.Fatal()
		}

		// await the outcome
		r := <-results
		if r.err != nil {
			t.Fatal(err)
		}
		t.Logf("%+v", r)
		t.Fatal()
	})

}*/
