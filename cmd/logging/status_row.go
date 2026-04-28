package main

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

var statusRowHeight = unit.Dp(50)

// statusRow renders the top status strip: full window width, fixed
// height, with a 1dp line along its bottom edge. widget.Border draws
// all four sides, so for a single-edge rule we paint a thin filled
// rectangle ourselves using clip + paint.
func statusRow(theme *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		// Force the row to take the full available width and exactly
		// statusRowHeight tall, regardless of its (empty) contents.
		gtx.Constraints.Min.X = gtx.Dp(windowWidth)
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Dp(statusRowHeight)
		gtx.Constraints.Max.Y = gtx.Dp(statusRowHeight)

		size := gtx.Constraints.Min
		borderPx := gtx.Dp(unit.Dp(2))

		// Clip drawing to a 1px-tall rect along the bottom edge, fill
		// it with the border colour, then pop the clip stack so we
		// don't bleed into anything painted after us.
		rect := image.Rect(0, size.Y-borderPx, size.X, size.Y)
		stack := clip.Rect(rect).Push(gtx.Ops)
		paint.ColorOp{Color: red500}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		stack.Pop()

		return layout.Dimensions{Size: size}
	}
}
