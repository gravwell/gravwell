package pathtextinput_test

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/pathtextinput"
)

func TestSuggestions(t *testing.T) {
	// generate tests and a testing function we can run against different PTIs
	tests := []struct {
		name            string
		input           string
		wantSuggestions []string
	}{
		{"empty input should retrieve all top-level suggestions",
			"",
			[]string{"dir1", "dir2", "file1", "file2", "file3"},
		},
		{"\"file\" omits dir* suggestions",
			"file",
			[]string{"file1", "file2", "file3"},
		},
		{"\"dir2/\" suggests all direct children of dir2",
			"dir2/",
			[]string{"file1", "fileA", "dirA"},
		},
		{"\"dir2/file\" suggests \"file1\", \"fileA\"",
			"dir2/file",
			[]string{"file1", "fileA"},
		},
	}
	testFunc := func(subT *testing.T, pti pathtextinput.Model) {
		for _, tt := range tests {
			subT.Run(tt.name, func(t *testing.T) {
				pti.SetValue(tt.input)
				pti, _ = pti.Update(tea.KeyMsg{
					Type:  tea.KeyRunes,
					Runes: []rune{},
				})

				// check suggestions
				actual := pti.AvailableSuggestions()
				// slice out just the "file" portion of each actual
				for i := range actual {
					_, actual[i] = path.Split(actual[i])
				}
				if !testsupport.SlicesUnorderedEqual(actual, tt.wantSuggestions) {
					t.Fatal(testsupport.ExpectedActual(tt.wantSuggestions, actual))
				}
			})
		}

	}
	// execute the actual tests
	root := generateDirectories(t)
	t.Run("rooted elsewhere", func(t *testing.T) {
		pti1 := pathtextinput.New(pathtextinput.Options{PWD: root})
		pti1.Focus()
		testFunc(t, pti1)
	})
	t.Run("rooted at .", func(t *testing.T) {
		t.Chdir(root)
		pti2 := pathtextinput.New(pathtextinput.Options{})
		pti2.Focus()
		testFunc(t, pti2)
	})
}

// Tests that the ti still works as intended when fed an absolute path.
func TestRemoteDirectoryAbsolutePath(t *testing.T) {
	root := generateDirectories(t)
	t.Run("remote directory", func(t *testing.T) {
		pti := pathtextinput.New(pathtextinput.Options{})
		pti.Focus()
		pti.SetValue(root + "/")
		pti, _ = pti.Update(tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{},
		})

		// check suggestions
		actual := pti.AvailableSuggestions()
		// slice out just the "file" portion of each actual
		for i := range actual {
			_, actual[i] = path.Split(actual[i])
		}

		want := []string{"dir1", "dir2", "file1", "file2", "file3"}
		if !testsupport.SlicesUnorderedEqual(actual, want) {
			t.Fatal(testsupport.ExpectedActual(want, actual))
		}
	})

}

func TestCustomTI(t *testing.T) {
	wantErr := "WRONG!"
	pti := pathtextinput.New(pathtextinput.Options{CustomTI: func() textinput.Model {
		underlyingTI := textinput.New()
		underlyingTI.Validate = func(s string) error {
			if s != "Baby Bee" {
				return errors.New(wantErr)
			}
			return nil
		}
		return underlyingTI
	}})
	pti.SetValue("Little Coco")
	if pti.Err.Error() != wantErr {
		t.Fatal("Validation did not return the correct error", testsupport.ExpectedActual(wantErr, pti.Err.Error()))
	}
}

func TestView(t *testing.T) {
	// setup
	pti := pathtextinput.New(pathtextinput.Options{PWD: generateDirectories(t)})
	pti.Focus()

	tests := []struct {
		name         string
		input        string
		wantFullView string // expected Value, with completion suffixed
	}{
		{"empty", "fi", "file1"},
		{"empty", "dir2/", "dir2/file1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pti.SetValue(tt.input)
			pti, _ = pti.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune{},
			})

			actual := pti.View()
			// cut off excess data from actual so it will actually match
			actual = strings.TrimPrefix(actual, " > ")
			if actual != tt.wantFullView {
				testsupport.ExpectedActual(tt.wantFullView, actual)
			}
		})
	}
}

//#region helper functions

/*
Creates a directory structure inside of t.TempDir:

	file1
	file2
	file3
	dir1/
	├── file1
	└── file2
	dir2/
	├── file1
	├── fileA
	└── dirA/
	    ├── fileA
	    └── dirZ

Calls t.Fatal if any step in the process fails.
*/
func generateDirectories(t *testing.T) string {
	root := t.TempDir()
	// Create root files
	touchFile(t, filepath.Join(root, "file1"))
	touchFile(t, filepath.Join(root, "file2"))
	touchFile(t, filepath.Join(root, "file3"))
	dir1 := filepath.Join(root, "dir1")
	if err := os.MkdirAll(dir1, 0755); err != nil {
		t.Fatal(err)
	}
	touchFile(t, filepath.Join(dir1, "file1"))
	touchFile(t, filepath.Join(dir1, "file2"))
	dir2 := filepath.Join(root, "dir2")
	if err := os.MkdirAll(dir2, 0755); err != nil {
		t.Fatal(err)
	}
	touchFile(t, filepath.Join(dir2, "file1"))
	touchFile(t, filepath.Join(dir2, "fileA"))
	dirA := filepath.Join(dir2, "dirA")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	touchFile(t, filepath.Join(dirA, "fileA"))

	touchFile(t, filepath.Join(dirA, "dirZ"))

	return root
}

func touchFile(t *testing.T, path string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

//#endregion helper functions
