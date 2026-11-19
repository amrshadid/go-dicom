package dataset

// SetRenderBandBytes makes the bands RenderFramesFrom reads about n bytes,
// so that a test can cut a small image into bands of a row or two, and
// returns what puts the size back.
func SetRenderBandBytes(n int) (restore func()) {
	was := renderBandBytes
	renderBandBytes = n
	return func() { renderBandBytes = was }
}
