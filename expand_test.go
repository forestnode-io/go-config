// Copyright (c) 2019 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/transform"
)

// Size of buffer used by the transform package.
const transformBufSize = 4096

const orig = `This is a $t3sT$. $$ This is a $$test.
	This is not a valid $0ne.  But this one $i5_@_valid-one.
	$$$$$$$
${parti`

const expected = `This is a test$. $ This is a $test.
	This is not a valid $0ne.  But this one is @_valid-one.
	$$$$
${parti`

const endsInDollar = `There is a dollar at the end$`
const endsInDoubleDollar = `There is a dollar at the end$$`
const manyDollarsOrig = `$$$$$$$`
const manyDollarsExpect = `$$$$`
const endsInVar = `There is a test at the end: $t3sT`
const endsInVarExpect = `There is a test at the end: test`

type oneByteReader struct {
	r io.Reader
}

func (e *oneByteReader) Read(buf []byte) (n int, err error) {
	var b [1]byte

	if len(buf) > 0 {
		n, err = e.r.Read(b[:])
		buf[0] = b[0]
	}

	return
}

type bufReader struct {
	buf    []byte
	offset int
}

// Similar to a bytes.Reader except this Reader returns EOF in the same
// Read that reads the end of the buffer.
func (e *bufReader) Read(buf []byte) (int, error) {
	var err error
	n := copy(buf, e.buf[e.offset:])
	e.offset += n
	if e.offset == len(e.buf) {
		err = io.EOF
	}
	return n, err
}

func TestExpander(t *testing.T) {
	r := bytes.NewReader([]byte(orig))

	expandFunc := func(s string) (string, error) {
		switch s {
		case "t3sT":
			return "test", nil
		case "i5_":
			return "is ", nil
		}

		return "NOMATCH", errors.New("No Match")
	}

	// Parse whole string
	tr := transform.NewReader(r, &expandTransformer{expand: expandFunc})
	actual, err := io.ReadAll(tr)
	require.NoError(t, err)
	assert.Exactly(t, expected, string(actual))

	_, err = r.Seek(0, io.SeekStart)
	require.NoError(t, err)

	// Partial parse
	var partial [11]byte
	tr = transform.NewReader(r, &expandTransformer{expand: expandFunc})
	n, err := tr.Read(partial[:])
	require.NoError(t, err)
	assert.Exactly(t, n, len(partial))
	assert.Exactly(t, expected[:n], string(partial[:]))

	// Empty string
	r = bytes.NewReader([]byte{})
	tr = transform.NewReader(r, &expandTransformer{expand: expandFunc})
	actual, err = io.ReadAll(tr)
	require.NoError(t, err)
	assert.Exactly(t, "", string(actual))
}

func TestExpanderOneByteAtATime(t *testing.T) {
	r := bytes.NewReader([]byte(orig))
	rr := &oneByteReader{r: r}

	expandFunc := func(s string) (string, error) {
		switch s {
		case "t3sT":
			return "test", nil
		case "i5_":
			return "is ", nil
		}

		return "NOMATCH", errors.New("No Match")
	}

	tr := transform.NewReader(rr, &expandTransformer{expand: expandFunc})
	actual, err := io.ReadAll(tr)
	require.NoError(t, err)
	assert.Exactly(t, expected, string(actual))
}

func TestExpanderFailingTransform(t *testing.T) {
	r := bytes.NewReader([]byte(orig))

	expandFunc := func(s string) (string, error) {
		switch s {
		case "t3sT":
			return "test", nil
			// missing "i5_" case
		}

		return "NOMATCH", errors.New("No Match")
	}

	tr := transform.NewReader(r, &expandTransformer{expand: expandFunc})
	_, err := io.ReadAll(tr)
	require.Error(t, err)
}

func TestExpanderMisc(t *testing.T) {
	tests := [...]struct {
		orig   string
		expect string
	}{
		{endsInDollar, endsInDollar},
		{endsInDoubleDollar, endsInDollar},
		{endsInVar, endsInVarExpect},
		{manyDollarsOrig, manyDollarsExpect},
	}

	expandFunc := func(s string) (string, error) {
		switch s {
		case "t3sT":
			return "test", nil
			// missing "i5_" case
		}

		return "NOMATCH", errors.New("No Match")
	}

	for i, tst := range tests {
		tst := tst
		t.Run(fmt.Sprintf("sub=%d", i),
			func(t *testing.T) {
				tr := transform.NewReader(
					bytes.NewReader([]byte(tst.orig)),
					&expandTransformer{expand: expandFunc},
				)
				actual, err := io.ReadAll(tr)
				require.NoError(t, err)
				assert.Exactly(t, tst.expect, string(actual))
			},
		)
	}
}

func TestExpanderLongSrc(t *testing.T) {
	a := strings.Repeat("a", transformBufSize-1)

	tests := [...]struct {
		orig   string
		expect string
	}{
		{"foo${a}" + a, "foo" + a + a},
		{"${a}foo$a", a + "foo" + a},
		{"$a${", a + "${"},
	}

	expandFunc := func(s string) (string, error) {
		switch s {
		case "a":
			return a, nil
		}

		return "NOMATCH", errors.New("No Match")
	}

	for i, tst := range tests {
		tst := tst
		t.Run(fmt.Sprintf("sub=%d", i),
			func(t *testing.T) {
				tr := transform.NewReader(
					&bufReader{buf: []byte(tst.orig)},
					&expandTransformer{expand: expandFunc},
				)
				actual, err := io.ReadAll(tr)
				require.NoError(t, err)
				assert.Exactly(t, tst.expect, string(actual))
			},
		)
	}
}

func TestTransformLimit(t *testing.T) {
	a := strings.Repeat("a", transformBufSize-1)

	// The transform package uses an internal fixed-size buffer.
	// These tests are expected to fail with specific errors when
	// that buffer is exceeded.  If they stop failing, then other
	// tests (above) have likely stopped working correctly too.
	tests := [...]struct {
		orig string
		err  error
	}{
		{"$a", transform.ErrShortDst},
		{"$" + a, transform.ErrShortSrc},
	}

	expandFunc := func(s string) (string, error) {
		switch s {
		case "a":
			return a + "aa", nil
		case a:
			return "a", nil
		}

		return "NOMATCH", errors.New("No Match")
	}

	for i, tst := range tests {
		tst := tst
		t.Run(fmt.Sprintf("sub=%d", i),
			func(t *testing.T) {
				tr := transform.NewReader(
					bytes.NewReader([]byte(tst.orig)),
					&expandTransformer{expand: expandFunc},
				)
				_, err := io.ReadAll(tr)
				require.EqualError(t, err, tst.err.Error())
			},
		)
	}
}
