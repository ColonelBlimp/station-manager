package main

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var bottomRowHeight = unit.Dp(380)

func bottomRow(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return widget.Border{
			Color: indigo600,
			Width: unit.Dp(2),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			c := layout.Constraints{}
			c.Max.X = gtx.Dp(windowWidth)
			c.Max.Y = gtx.Dp(bottomRowHeight)
			return layout.Dimensions{Size: c.Max}
		})
	}
}
