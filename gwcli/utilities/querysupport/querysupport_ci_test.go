//go:build !ci
// +build !ci

package querysupport

import (
	"path"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	. "github.com/gravwell/gravwell/v4/gwcli/utilities/testingsupport"
)

const ( // testing server credentials
	user     = "admin"
	password = "changeme"
	server   = "localhost:80"
)

// NOTE(rlandau): this test is fairly brittle, as it relies on the backend being in place and the gravwell tag having consistent columns.
// But some testing is better than no testing.
func Test_GetResultsForDataScope(t *testing.T) {
	// clilog needs to be spinning
	if err := clilog.Init(path.Join(t.TempDir(), t.Name()+".log"), "DEBUG"); err != nil {
		t.Fatal("failed to spin up logger: ", err)
	}

	// connection to the backend needs to be spinning
	// establish connection
	if err := connection.Initialize(server, false, true, ""); err != nil {
		panic(err)
	}
	if err := connection.Login(connection.Credentials{Username: user, Password: password}, true); err != nil {
		panic(err)
	}

	{ // submit a query to make sure that there is data to be picked up for future tests
		qry := "tag=gravwell | limit 1"
		s, err := connection.Client.StartSearch(qry, time.Now().Add(-time.Second), time.Now(), false)
		if err != nil {
			t.Fatal(err)
		}
		if err := connection.Client.WaitForSearch(s); err != nil {
			t.Fatal(err)
		}
		s.Close()
	}

	t.Run("table query", func(t *testing.T) {
		var columns = []string{"SRC", "TIMESTAMP", "DATA"}
		var columnsString = strings.Join(columns, ",")
		// create a search with the table output module
		qry := "tag=gravwell | table " + strings.Join(columns, " ")
		s, err := connection.Client.StartSearch(qry, time.Now().Add(-time.Minute), time.Now(), false)
		if err != nil {
			t.Fatal(err)
		}
		res, tbl, err := GetResultsForDataScope(&s)
		// validate outcome
		if err != nil {
			t.Fatal(err)
		} else if !tbl {
			t.Fatal("expected table mode to be true, was false")
		} else if len(res) == 0 {
			t.Fatal("found no data in prior minute despite successfully submitting a test query")
		} else if res[0] != columnsString {
			t.Fatal("incorrect header row/columns.", ExpectedActual(columnsString, res[0]))
		}
	})
}
