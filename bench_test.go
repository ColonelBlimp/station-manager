package logging

import (
	"io"
	"strconv"
	"testing"

	smerrors "github.com/Station-Manager/errors"
	"github.com/rs/zerolog"
)

// newBenchService constructs a Service with a discard logger at the given level.
// It bypasses Initialize() to avoid I/O setup and focuses on pure logging overhead.
func newBenchService(level zerolog.Level) *Service {
	s := &Service{}
	logger := zerolog.New(io.Discard).Level(level)
	s.logger.Store(&logger)
	s.isInitialized.Store(true)
	return s
}

func makeDetailedChain(depth int) error {
	if depth <= 0 {
		return nil
	}
	err := smerrors.New(smerrors.Op("op_0")).Msg("root cause message")
	for i := 1; i < depth; i++ {
		op := "op_" + strconv.Itoa(i)
		err = smerrors.New(smerrors.Op(op)).Err(err).Msg("wrapped message")
	}
	return err
}

func makeStdWrapChain(depth int) error {
	if depth <= 0 {
		return nil
	}
	err := smerrors.New(smerrors.Op("std_root")).Msg("root cause message")
	for i := 1; i < depth; i++ {
		op := "std_" + strconv.Itoa(i)
		err = smerrors.New(smerrors.Op(op)).Errorf("wrap %d: %w", i, err)
	}
	return err
}

func BenchmarkInfoWith_NoErr(b *testing.B) {
	s := newBenchService(zerolog.InfoLevel)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.InfoWith().Str("k", "v").Int("n", i).Msg("hello")
	}
}

func BenchmarkErrorWith_DetailedChain3(b *testing.B) {
	s := newBenchService(zerolog.ErrorLevel)
	err := makeDetailedChain(3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.ErrorWith().Err(err).Msg("oops")
	}
}

func BenchmarkErrorWith_DetailedChain6(b *testing.B) {
	s := newBenchService(zerolog.ErrorLevel)
	err := makeDetailedChain(6)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.ErrorWith().Err(err).Msg("oops")
	}
}

func BenchmarkErrorWith_StdWrap6(b *testing.B) {
	s := newBenchService(zerolog.ErrorLevel)
	err := makeStdWrapChain(6)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.ErrorWith().Err(err).Msg("oops")
	}
}

func BenchmarkParallel_InfoWith(b *testing.B) {
	s := newBenchService(zerolog.InfoLevel)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.InfoWith().Str("k", "v").Msg("hi")
		}
	})
}

func BenchmarkParallel_ErrorWith_Detailed3(b *testing.B) {
	s := newBenchService(zerolog.ErrorLevel)
	err := makeDetailedChain(3)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.ErrorWith().Err(err).Msg("oops")
		}
	})
}
