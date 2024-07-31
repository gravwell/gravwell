package stylesheet

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	baseCell = lipgloss.NewStyle().Padding(0, 1).Width(30)

	Tbl = struct {
		// cells
		HeaderCells lipgloss.Style
		evenCells   lipgloss.Style
		oddCells    lipgloss.Style

		// borders
		BorderType  lipgloss.Border
		BorderStyle lipgloss.Style
	}{
		//cells
		HeaderCells: lipgloss.NewStyle().
			Foreground(PrimaryColor).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).Bold(true),
		evenCells: baseCell.
			Foreground(row1Color),
		oddCells: baseCell.
			Foreground(row2Color),

		// borders
		BorderType:  lipgloss.NormalBorder(),
		BorderStyle: lipgloss.NewStyle().Foreground(borderColor),
	}
)

// Generate a styled table skeleton
func Table() *table.Table {
	tbl := table.New().
		Border(Tbl.BorderType).
		BorderStyle(Tbl.BorderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == 0:
				return Tbl.HeaderCells
			case row%2 == 0:
				return Tbl.evenCells
			default:
				return Tbl.oddCells
			}
		}).BorderRow(true)

	return tbl
}
