//go:build !race
// +build !race

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datascope

// CI-compatible testing for Datascope.
//
// Relies on TeaTest, which relies on Golden files
// Regenerate the associate golden file with: go test ./tree/query/datascope -run ^Test_ -update

import (
	"path"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

const (
	termWidth  int = 80
	termHeight int = 50
)

// A basic test that spins up DS and immediately shutters it to confirm it still conforms to our expected output.
func Test_Simple(t *testing.T) {
	// create some dummy data
	data := []string{
		"Line 1",
		"Multi\nLine2",
		"Line 3",
	}
	// perform initial setup
	_, tm := setup(t, data, false)

	// check the final output
	TTSendSpecial(tm, tea.KeyCtrlC)
	TTMatchGolden(t, tm)
}

func Test_TabCycle(t *testing.T) {
	data := []string{
		"Line 1",
		"Multi\nLine2",
		"Line 3",
	}
	_, tm := setup(t, data, false)

	// perform a full cycle and then continue to the second tab to ensure we actually moved
	TTSendSpecial(tm, tea.KeyTab)
	TTSendSpecial(tm, tea.KeyTab)
	TTSendSpecial(tm, tea.KeyTab)
	TTSendSpecial(tm, tea.KeyTab)
	TTSendSpecial(tm, tea.KeyTab)

	TTSendSpecial(tm, tea.KeyCtrlC)
	TTMatchGolden(t, tm)

	// also want to check the state of the final model
	finalDS, ok := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(DataScope)
	if !ok {
		t.Fatal("failed to cast final model to datascope")
	}
	if finalDS.activeTab != 1 {
		t.Fatal("expected DS to end on second (zero-indexed) tab", ExpectedActual(1, finalDS.activeTab))
	}
}

func Test_MultiPage(t *testing.T) {
	// perform initial setup
	_, tm := setup(t, loooongData, false)

	// cursor through the pages
	TTSendSpecial(tm, tea.KeyRight)
	TTSendSpecial(tm, tea.KeyRight)
	TTSendSpecial(tm, tea.KeyRight)

	TTSendSpecial(tm, tea.KeyCtrlC)
	TTMatchGolden(t, tm)
}

// This test replicates the simple test above but with a different color scheme.
// This is more or less redundant as lipgloss will corral the colors down to what the tty supports and
// the "tty" in this case (the file) is probably considered a single bit.
func Test_SimpleColor(t *testing.T) {
	data := []string{
		"Line 1",
		"Line 2",
		"line 3",
		"\n\n\nLine\n4",
	}
	// manual setup to set a different color scheme
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.Classic()
	// create a dummy search that should work so long as we don't trigger download or schedule
	search := grav.Search{RenderMod: "text"}
	ds, cmd, err := NewDataScope(data, false, &search, false)
	if err != nil {
		t.Fatalf("failed to create datascope: %v", err)
	} else if cmd != nil {
		t.Fatalf("datascope should never return a command if it knows Mother isn't running. Returned command: %v", err)
	}
	// spin up the teatest
	tm := teatest.NewTestModel(t, ds, teatest.WithInitialTermSize(termWidth, termHeight))
	// check the final output
	TTSendSpecial(tm, tea.KeyCtrlC)
	TTMatchGolden(t, tm)
}

func Test_SimpleTable(t *testing.T) {
	data := []string{
		"Col1,Col2,Col3", //header
		// data start
		"A1,B1,C1",
		"A2,B2,C2",
		"A3,B3,C3",
		"A4,B4,C4",
		"A5,B5,C5",
		"A6,B6,C6",
	}
	_, tm := setup(t, data, true)
	// check the final output
	TTSendSpecial(tm, tea.KeyCtrlC)
	TTMatchGolden(t, tm)
}

// shared helper function that returns datascope and teatest models ready for use.
func setup(t *testing.T, data []string, table bool) (DataScope, *teatest.TestModel) {
	t.Helper()
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.NoColor()
	// create a dummy search that should work so long as we don't trigger download or schedule
	search := grav.Search{RenderMod: "text"}
	ds, cmd, err := NewDataScope(data, false, &search, table)
	if err != nil {
		t.Fatalf("failed to create datascope: %v", err)
	} else if cmd != nil {
		t.Fatalf("datascope should never return a command if it knows Mother isn't running. Returned command: %v", err)
	}
	// spin up the teatest
	tm := teatest.NewTestModel(t, ds, teatest.WithInitialTermSize(termWidth, termHeight))
	return ds, tm
}

// NOTE(rlandau): this has to be pregenerated because it must stay consistent between runs (lest the golden files not match).
var loooongData = []string{
	"SktrHBrVozw2TdYB8oCGzjzB2 7X2g0lXPCNOQeBpq4OZPs6AHx WpILfvsnH6cDISL7E2tdqjE5V 31hR2kUJePDNXwS2PHvO83Uez gcuSFRgH5sqgaFeVQxEXeyGlx 85harsi0q3e2hW9LZzL3uHY6k ZLJpDJV8hJNI6nqkyLFINZtWr",
	"j04k4fg3ehOD59CHOyGz7xPXX",
	"mjGaa5RCU5A9w83IxWJeBF0fh",
	"81YRtH8iZuD9rD3JCp2r8lifB",
	"VaUFftLkKEyx7rZ701I583N14",
	"QWu5pU0QxHggCR8rGe5ypAG24",
	"EikS8zLWXfPiJzClZD3fbQmgX",
	"rrN4oSHdlw94u1TohCvk20BG0",
	"6rftTDJDCAVgFA58ZVF5dmGjF",
	"njTpVxCjUBhcedefhqDlDUUQ9",
	"5QuGc1CXs36J7wERe8Xdqbd5G",
	"l4h4l7Km3p2qo51dpqZ8CjgkU",
	"3OVs9vzPoT6X5t7n3UsJenLqE",
	"e70pv3rxzBqGHLXRFvJdJ9pHw",
	"k57WhSCDyXhG0OXQ5OSJUsuNu",
	"CV9yuKAgqWetGjhHIxaGPhNYr",
	"EfkiTIfDBnu88MLigPASUHhgd",
	"Pu5bxYtSwpin45hMhSsc2C8GM",
	"6jD7u7s2bLJItr3vFzV8DLmeE",
	"wRIu3mUpQBIZwvPCEfZoXIMBt",
	"1121OkQKcLS6P7vGsaaixDTaQ",
	"a9phkCyN90QRyzqWLVsPGJEmV",
	"TrmKRtI3tvqdrRTBn7Aum4TWS",
	"uN2iEu51XzslHzA80uXQHvcDv",
	"vQoC1yvvI6dUXiVfGpHHl7B5U DouIACd9SDhC9mCyC5Q148M1k CF3JhBqb5J7xEb3fg1oA5VeGS OsovUHtUjJtYptseD6GNPdpHH 5FYgG6FbNmtpW4zU35pOLX00c Ki6kZeb7JiIpqizvE1Nw25lxY",
	"afcAPzSUZzCBM7oNoZmhbRSSF",
	"4bfWdSShvy144cItrjnjuLoLD",
	"iiPqA7MJYcloN2ITQPLcNbkmO",
	"L3Uc3T4FxX4RYTNoeW8gmv1ia",
	"Tt9oXeJn2PsPTgwa4hDgRNrpp",
	"Rs0N2vR0ulbTN31smhA7jGKI2",
	"vbNh2JRx3tqt90TU1cZj1pU88",
	"SSBn7xcsuoN1ucrZ35WT4JpRN",
	"3g3zo4x6NSVHVbu0U104CDZRH",
	"USOJJ6Gqib4KWjtiBas3XvyjJ",
	"ubcS9QkN788hWOH4JRQMVr8Xy",
	"hbhsTi2IHZCirdX8FIukKUQrj",
	"NUD2SOv3Fc4lgDpOSkhwtowCH",
	"IGynmQ65IGniAoyHFgTDZB3Ak",
	"8tdEesQ2aeTqZwriSTm4uXiQs",
	"cyMinCQVSOpe9HWAjqYeQuujE",
	"3OYkaPaYF0fwjPcFuguLLZ1v1",
	"XzC5W2kRXOmvabgjpseMajosW",
	"VnzMGKfzeoIYNbiAS6NJ08Oau",
	"TbR4cieqAgoJFJy5CmizGGiJx",
	"o2P8mqHW1iDWKedjpo7xcxXKs",
	"g184ZWZpCIyRqQG7qwVRkI8QN",
	"coYo69OjpFWbzvSVFHMcyKl9n",
	"6B0chCxwx4idGO7VhSDZzSRS3",
	"c5TaL5AgCJkwTHIDrfMKo6IGu",
	"wWTQofyklR6YvN8z53SpdTCC8",
	"HMGxYjZYqi0A6TfUKtmusKAKI",
	"UWhy8gZ3A36F3s7K986462uQr",
	"P7APq2YhE8zKudTtjmeKu5j29",
	"OxAdbzSL4oeSccHzLZobBHbEv",
	"fHNFOPawP7DDo28cUUfUTAT1a",
	"EiRGWasyCx2fYTC9AkxePwYbS",
	"8nqbodWV1Ydj5qP5Nt9ltzmZf",
	"FkkXqRrsaSZhVkGeEmziJpZby",
	"ZCuq8Q8fTPDTSCq1ldZ23VgWS",
	"SqI8MzPGMa3kCU6sJfhajlP9H",
	"znGqwbxGlNl0Bxuu6pyjFTh8P",
	"oU0xh6F1aZ1WQFpuyHZQqtQI1",
	"pvDsBoGbj9Y84WnseWZQpt9i2",
	"VOxcizIjPcV6105Gy5lMGNMwk",
	"WRKbQRjF3AVBdKuwxIx4j6ykX",
	"gnk4lR29abmTmV9MAmNkPRMFb",
	"z2Jawu7ugIjl6hmDJV1uUHpF4",
	"lJ1VNAJfFxLX0p2p8zMdOofBK",
	"Lu47rvCZB7Ulgf0pwXiygtawQ",
	"lnsisvqWBx33U6J6Qx64pTrO8",
	"G0e3flhK86NqgblGhZYoC7pWH",
	"oiiVMn0vw3Wh6nReuDPU3D693",
	"zpcl9hcBh72qeJDiflNTWUrNG",
	"a6GlBy5jPjDjnPwVaERk2fnKF",
	"EXs60VC5nmIyGp6n7GnoxJK7e",
	"HbxFSmraxNVXc59JINavYUrve",
	"wE93DlWqNxVKt07k3wRFo45up",
	"GqypjcOuk1T3EO2GJp0nPsLsW",
	"n2y0mikxDbn4U9JjiQlqDbs1q",
	"cnWGMtt4q9UjWfZshwC3yIUNA",
	"AoTmiqxaSMLE45FZXx5dAb9KH",
	"dt861SLBtdTsxo83gGFmGe5Wu",
	"uycycVJvTv6TBpbBJxN2pmvAw",
	"K8mRv5O7kBB0kBce8gSBVBPXx",
	"xmSezural3D65WLiQVEGcmtTV",
	"HcdpaYCziMsEa4KI3wiTY5FPJ",
	"tcmgoJQfHrUBvTOtSvWcaRor1",
	"QXFp3FFSyUcnCrf5yx7y8Fh42",
	"egjNt6Z6YvJCyO5ivRGoMuB05",
	"EMIpOH7lggphLEaqv6fjPmdDR",
	"wSyjeCYHTcDgHeRnftgPzYxVX",
	"AR5raoieKttZzv3kUnLS9CaKN",
	"6f1ewJJC7xJ2j1Jj3id6fRwX5",
	"m4OEtpPb6iS3RVanFBaLeLd0i",
	"7HXbLuLPjhQbfjjiKrPzwv6UP",
	"evARaFTeRqMv30p2OGIwMzlXd",
	"2Fh7HD5gWSbK2VM5jGAWu7EL7",
	"qxBDJtV0UlxSmjxb7oQDw3jIT",
	"krujFUCKoYeHkeYwrCQO731Hy",
	"En6m53TXcf2lIszRtRvdn4h8T",
	"LvuaAdzFwDOcjhflzhU6vU4MT",
	"wZ6Yzv1dkwtO4K5TPigYxTk7t",
	"YfSptOZpsWMVJnODYjEvj15wM",
	"DV6GIAiqKX1929Ol6vDHTX5Zf",
	"cBVMpBFiO1P8pZist6t7wd89Q",
	"RrrnnwDcuCSq1SN3ThwrVCUsu",
	"tJ0TOFkHEW3tNIYgl8ZTQkjl0",
	"2REXgKL3gSZxNcBjMxD3RIpmp",
	"Py9GWA1KArvAUfyfWGI306knS",
	"lg1CrRQJh8p6aX1IR9MprQBle",
	"zSDy253tIZokp6dES8BP4PADb",
	"QH8k8BMCu9GaQqHWRinDtyXb5",
	"xmJuAxFxCm8R2CSVhk0wtDHjU",
	"wHX1cyD4NmjUTG3KS4myPw24T",
	"t9usBjF4Qyqq7ly9L4WRGvGGK",
	"rSeHu7Oc5yz5YFVQj9y4hdor0",
	"s4V1wwP18sjlrZ2sGVarlTpWM",
	"oOzubz9FEZlwE1Fyrk4LIJfUi",
	"Rmiwl0KU3hqCu3kBo4XaUpC6V",
	"q2ANQAnZAoIYwHxFzT0RGl27l",
	"oZYh8ganF02ft7fRamZsNQjGf",
	"949hukrgnCusbXLXQVURIbz4X",
	"NsIbYV0yTaOq0CTyYug8oBG7a",
	"7uxbIVBJOuRTwxVptujNesTTL",
	"f39Ij5mLTezrS6xKYaGpZNmy7",
	"tgW0R5qvGGEhIgYAtlcLGgvl8",
	"9CcmPJ2GtWqQIInT7eTj4XH38",
	"YcES80bX2jFNqr08VJVPb6Zd3",
	"FvAm5NDXZd5DHTkDQnSahsct7",
	"83ce84rzEJUU5zo0CofyC8TvS",
	"tG6siF8Zh02F2XouQ0WuvYyTJ",
	"0dMtzMrZ62XGiJ1RRSaVZXy4T",
	"JuXWoKCxnPUVFNfzBP07FrFHb",
	"YIeOEY6x8Ps6M5TRY1zsWwPY0",
	"ObYykS5Iqbaiml1nsYayeitqu",
	"I7FAHvMA3MR6mtZJHtXSjhX3l",
	"jE6BPZ9V9mdHa8swbspz9ueu8",
	"FK2DY1qAE8e9XAg4FugisUuDX",
	"6F0WlAfVNGuzAzJTI3vBjlxIt",
	"2JauLfRwOwx56cvPUiWB0ah1Y",
	"6LDTzb5GMefWDusCSBO3zquy4",
	"yb5L7KvvTgOHrdTtJCLqODhhn",
	"2kTUITnmt2nuOFsx8VAcFvrSC",
	"UUqLdIOrPZI9bQLI3hS55t90N",
	"FxAA1i54Q8JXwEmUTa1mwgy5V",
	"lwfluyccU9ryO7K9enYJaKWZk",
	"7Jne48GFVVaAvlaOkeRMlcQTU",
	"im1JOEpdjMg1O6ZKHHLMCigzH",
	"SILCS2DP2fEFSw6i11enZK7l6",
	"1j9gS2GzHxBffF3bTBh4WK5kL",
	"SnEo5JfHOKAD6AQ1KjojiYZQM",
	"Y0bbB3j1a8BviCDc0YDVTstws",
	"Ek3lhQ3p27RNCCjPY27AgOZH3",
	"eRP7oBK0ynlkJcPywJRAbg3i2",
	"SrBdkYMbo1xiTZBmf064YBEtz",
	"0vduV43viXXPtD4jxmUiXlJsk",
	"PPknXKLKi7gxWBwFdjiXjKQjM",
	"ABg0jfj366l69oTf9jB8nq54F",
	"xXiEoRJAov73S6awCVATNMsCT",
	"paFPO2zKNkCJaSw9jdDmoZslU",
	"UtvZPexyWMjRCtjXFG4Gu5Ckj",
	"OqvMR08N7PAmy9wgiUJdLb87A",
	"FmdLO1UureLIWqa4mqmkktMTp",
	"Bj6rRNo4zaaTjY3ZiRMDdhQ29",
	"6OgRkM4uMuLJRUfiGbOLb1jf9",
	"V4Uwo2IwiaHtxyTz0fC3gntj6",
	"i2uX7Strca7XVyLvxRIFDCor6",
	"ivdXLx3KcAVeLZyL1KzQjD896",
	"AjDMSjPVwLNSJl2T1gXv06kcJ",
	"bdxKJHmwpGDCq9UyIxvjVrtmj",
	"Vcdoh4NLbO40p9kL08UQTuAPW",
	"Oyc3cT3W5lPgA1PZfJAhq4kvM",
	"a4AEJbTpzbGqY2cvfLCTNpl4E",
	"1Qjy2StGggn9Br0WiEdkQntJN",
	"gOB50SAkGSlscg4IoPGgFvplL",
	"KHXC4BcZtCcVx82e8oArzjXCN",
	"IVyVlpBFbnylk6MrJAExtKYZN",
	"XLFLexGw8z47mrlNibCjRJFW8",
	"vDXy5TaiJLTJyFE6S4O0VgvvY",
	"ox24ofD57sh9xuczKykDt3nBG",
	"dH1EP416CDmGQoQ6hhokf9K1f",
	"2PsDlQChsoOaoJfpHWzb1DNQF",
	"vSBstaVqDgdXT780gCfVbMXdu",
	"WXfo391rmNJyH3geP9Z35nLrG",
	"A3Er2tvqRRbt2AsIuCVVONCTP",
	"TbsAu1gwrHvMsNhpbodU5aP1o",
	"1tWY0rwPJ1UKXoWDRJYph63SU",
	"XI7qEDglY8GiSe1wavEw02InR",
	"Gy2uePW5b2M0aggOyymbug2f5",
	"7yVVy71hiBYSo0MeBjkL0HgMP",
	"TQ8dqyt3wnchIMrHfE9whqQiO",
	"HKEI2lnQRvig0ZysYBbICrZdU",
	"Dghl20sINCza7gpRdu6uUB5PD",
	"kLAl71rTVs4fD3JC84JOn4Vpp",
	"ghX8KEFgDoxwCqdP4OtcBDUoX",
	"PgqVK6DJyeMslvq4WtLMHixpc",
	"NikWZAUlo1H0LPevzH8vZQog0",
	"9BET3BLE4oxRWbLck9WfQ2804",
	"3lHySegL2PiIT2CyRwyctQpvW",
	"PmaB92RMk6VBpshOFNfOSsapt",
	"rKTxxUuLdByqBYeMMaZmVOv3r",
	"3TuNob5F5wBGXq22U1jjopHNB",
	"45f6AxZFFfshbC8s6XlCULkHI",
	"YCd9ePDkyVHceq7dzVpthrJFH",
	"TP28mSEnTPpRhntx8OzzvkVVk",
	"IO1WIHBrBwcQyj2Tz5FqCLgpZ",
	"ee80msOTelUhUQhClFF1HnH9m",
	"sYRD9jtBqAWwvk9RpyvvDqf2W",
	"jPbdUZiESmo0dWD7JnyfzbRvL",
	"Z4SJ4uWxSmWOWJ1nObKb8yNyg",
	"rdc7nGDQuNrxIymGFvFaJP0pm",
	"gsEvvbdiYQwsLAZe5IvJb5k3w",
	"2N1s9I9j5YxiFCCliKzIuSHBF",
	"hy1NJlnuAqUCTgSMp09rkDgAS",
	"1IjCnxGXOI3d52f8HYtQATAji",
	"6Yexcp6lZ0UylCaE8t1LGBeq7",
	"9FUGGkeTw9Duq7Ews7C2Bii7W",
	"PT5vINdwsGV1lVlQguVxlx8jT",
	"AEnFIwYslnNZDZD5a4jIuSdna",
	"u0O4LAxdSKnpymEJqR7XhX62a",
	"dpm0eH6SEitRbCmhxCo9EK3H7",
	"nnnZBCdl0KGuOWpbRei9feZrP",
	"JpeTfEIr0drNrJ5ZS26VxsAjz",
	"zMrAVJqIPZGnSOdbPem4VRqMY",
	"8q8zwg2KXfcJFVaxYf1MMrnKf",
	"W1hn4JFpbOuFEdnHDq1ikmHpW",
	"yzyNGxK6z8O51X4Ec1GSptC2b",
	"XP8D3im8vNVNHOFfLtYo6ZgDf",
	"W5B8b6jg9CZ43sTd0znGkjptI",
	"r9KoK9N2FE06qqIP87CJgh2lx",
	"tGYbO5cjdnmTx30zfkSHzy2nn",
	"mM7J0rZUaVYYVAs2agtbcAPK7",
	"5mFAmHKe7X0i0aUAcsYfF8dBi",
	"vImkf5Br0MuaLG2vbzEm7kqVK",
	"EAaP429ZMHMq9zC34sYsaWJq8",
	"wFBNshEArrlQ8Fv7MrVVhZ1Bk",
	"oJ7tXY0gN2TQI7WTFlSc5OoDK",
	"SUfKZKBIsYFGgjVT1JiTnMlBy",
	"kZOh54yPJtWU4RPrSDEc05V5S",
	"MHuxeQrfNwPVz1lr5cdX6zoel",
	"OwlL0yYxjc9kQvK1SpLuwnQ6X",
	"rDg2wioUXxsJYSrW8ytDB89GV",
	"WcwX3WKCiJfuHOX5S1BPySkad",
	"2tfq7XafaBjYnhzaC7kiVrmYy",
	"Re9WJKpwkLk2uil7AWqLKJrim",
	"Tv3UKxv1umeiyR2PxWy7S7MxK",
	"ZeVQUEpi32zX5PI8DXjww5ZGW",
	"RW4lMV3NAvmX1nhl2Nj1nL4Kr",
	"NzlYID54Yh6MZ05JPbeJ1OWJK",
	"yqapeArT7o4FCxQUHmyKypki6",
	"dosfcax0x43flKWNTu3vw30H7",
	"tOFBsniBiLiCdkoogwUbYB5uY",
	"wpgS0aZYTp86bcrQcAsvcmiFg",
	"pbRqyTdjCGO6TVNB8ensZQ5rh",
	"uEtYflX12VVKdXDROKm0N0R7A",
	"3yddSTwLv1CU6rnRmUjFo6cKx",
	"0UOifKhF97c4lp0nGWnnO9LT2",
	"OfYXTjnEhW1Sb9b9Hc4sVWZip",
	"vLKo2sNpMDXanrVsawOVECL2F",
	"6ChAFtyzYDF0LWLorylLqwXsn",
	"Vh5u8xjW7pios63uu2HIp9R1D",
	"j8BaGKgg9VzInTzX9REyqGMk5",
	"nLsNi8v3yf8gLZSIV1PeVAIMg",
	"jNwdCQwtcFtft73Mv8wqaDgkn",
	"QNenk5JGQFufG3kmKSi6QQWB8",
	"hnCv4eoB55a1kf4GZtS4YAJG2",
	"XVdXxpnTCwjKRxS2vF3MEWLlm",
	"slkcJuXyLB6rNEel9fwXLgMUH",
	"AlZGamsu49RnhCcwev0pZyw2k",
	"VAlWsGj0QjAQY41oW209dQFIn",
	"HOvogf4nfgKxwcRG5yNoJRMgd",
	"hEFXSQZWVwUi0xDhHld7Fl1ri",
	"wcoOjvofbxss2P390CKbpgRVu",
	"hGeAEnPykWtBbjB1k8rHS6A4q",
	"TSsGJWqW6w5bPH3EQ1z8xbccD",
	"CWgn71toNKGHmOJSJrCfa6vw1",
	"ZGDmnvoJyMelI29krR0jGhDI5",
	"mN6UZ9whtcVd3gOYb2CMqEJSm",
	"tYPFoSGiSqs1TTXu7OFJYDAet",
	"3Eqze1gktHJdfdeSEnbIC87Qy",
	"hG8zsXot2EOEIcCfbWeG4ptKE",
	"fG0nUTYEZFqUFVN7e7Hies5No",
	"XCFKRIRqfRXPdPfwGMoiGJtGA",
	"mqasqr47WELe4rR5heGhmPzCO",
	"TxhdBRmt5KiwNg4j7AmmSemwI",
	"XTJmnLSEo5xI6rLwvl2ttIKmp",
	"hXQegU4nIziqHwMVG06m05upS",
	"TSySJzI5tufc3iBkzp3XflzLC",
	"Q6NP21S1WOARqnszwV69vBvNb",
	"M4BeU40nyANm3vUpWOhfUPUFp",
}
