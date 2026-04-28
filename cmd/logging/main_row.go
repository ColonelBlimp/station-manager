package main

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var mainRowHeight = unit.Dp(400)

// mainRow renders the row directly below the status row: full window
// width, fixed height, framed by a 1dp blue border. The row is split
// horizontally into a 2/3-width left panel and a 1/3-width right
// panel using a Flex with weighted (Flexed) children.
func mainRow(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return widget.Border{
			Color: green600,
			Width: unit.Dp(2),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			c := gtx.Constraints
			c.Max.X = gtx.Dp(windowWidth)
			c.Max.Y = gtx.Dp(mainRowHeight)
			return layout.Dimensions{Size: c.Max}
		})
	}
}
