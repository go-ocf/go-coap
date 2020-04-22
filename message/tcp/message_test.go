package tcp

import (
	"bytes"
	"testing"

	coap "github.com/go-ocf/go-coap/v2/message"
	"github.com/go-ocf/go-coap/v2/message/codes"
	"github.com/stretchr/testify/require"
)

func testMarshalMessage(t *testing.T, msg Message, buf []byte, expectedOut []byte) {
	length, err := msg.MarshalTo(buf)
	if err != nil {
		t.Fatalf("Unexpected error: %d", err)
	}
	buf = buf[:length]
	if !bytes.Equal(buf, expectedOut) {
		t.Fatalf("Unexpected output %v, expeced %d", buf, expectedOut)
	}
}

func testUnmarshalMessage(t *testing.T, msg Message, buf []byte, expectedOut Message) {
	length, err := msg.Unmarshal(buf)
	if err != nil {
		t.Fatalf("Unexpected error: %d", err)
	}
	if length != len(buf) {
		t.Fatalf("Unexpected length decoded %d, expected %d", length, len(buf))
	}

	if msg.Code != expectedOut.Code ||
		!bytes.Equal(msg.Payload, expectedOut.Payload) ||
		!bytes.Equal(msg.Token, expectedOut.Token) ||
		len(msg.Options) != len(expectedOut.Options) {
		t.Fatalf("Unexpected output %v, expeced %v", msg, expectedOut)
	}

	for i := range msg.Options {
		if msg.Options[i].ID != expectedOut.Options[i].ID ||
			!bytes.Equal(msg.Options[i].Value, expectedOut.Options[i].Value) {
			t.Fatalf("Unexpected output %v, expeced %v", msg, expectedOut)
		}
	}
}

func TestSizeMessage(t *testing.T) {
	var msg Message
	require.Equal(t, 2, msg.Size())
	msg = Message{Code: codes.GET, Payload: []byte{0x1}, Token: []byte{0x1, 0x2, 0x3}}
	require.Equal(t, 2, msg.Size())
}

func TestMarshalMessage(t *testing.T) {
	buf := make([]byte, 1024)
	testMarshalMessage(t, Message{}, buf, []byte{0, 0})
	testMarshalMessage(t, Message{Code: codes.GET}, buf, []byte{0, byte(codes.GET)})
	testMarshalMessage(t, Message{Code: codes.GET, Payload: []byte{0x1}}, buf, []byte{32, byte(codes.GET), 0xff, 0x1})
	testMarshalMessage(t, Message{Code: codes.GET, Payload: []byte{0x1}, Token: []byte{0x1, 0x2, 0x3}}, buf, []byte{35, byte(codes.GET), 0x1, 0x2, 0x3, 0xff, 0x1})
	bufOptions := make([]byte, 1024)
	bufOptionsUsed := bufOptions
	options := make(coap.Options, 0, 32)
	enc := 0
	options, enc, err := options.SetPath(bufOptionsUsed, "/a/b/c/d/e")
	if err != nil {
		t.Fatalf("Cannot set uri")
	}
	bufOptionsUsed = bufOptionsUsed[enc:]
	options, enc, err = options.SetContentFormat(bufOptionsUsed, coap.TextPlain)
	if err != nil {
		t.Fatalf("Cannot set content format")
	}
	bufOptionsUsed = bufOptionsUsed[enc:]

	testMarshalMessage(t, Message{
		Code:    codes.GET,
		Payload: []byte{0x1},
		Token:   []byte{0x1, 0x2, 0x3},
		Options: options,
	}, buf, []byte{211, 0, 1, 1, 2, 3, 177, 97, 1, 98, 1, 99, 1, 100, 1, 101, 16, 255, 1})
}

func TestUnmarshalMessage(t *testing.T) {
	testUnmarshalMessage(t, Message{}, []byte{0, 0}, Message{})
	testUnmarshalMessage(t, Message{}, []byte{0, byte(codes.GET)}, Message{Code: codes.GET})
	testUnmarshalMessage(t, Message{}, []byte{32, byte(codes.GET), 0xff, 0x1}, Message{Code: codes.GET, Payload: []byte{0x1}})
	testUnmarshalMessage(t, Message{}, []byte{35, byte(codes.GET), 0x1, 0x2, 0x3, 0xff, 0x1}, Message{Code: codes.GET, Payload: []byte{0x1}, Token: []byte{0x1, 0x2, 0x3}})
	testUnmarshalMessage(t, Message{Options: make(coap.Options, 0, 32)}, []byte{211, 0, 1, 1, 2, 3, 177, 97, 1, 98, 1, 99, 1, 100, 1, 101, 16, 255, 1}, Message{
		Code:    codes.GET,
		Payload: []byte{0x1},
		Token:   []byte{0x1, 0x2, 0x3},
		Options: []coap.Option{{11, []byte{97}}, {11, []byte{98}}, {11, []byte{99}}, {11, []byte{100}}, {11, []byte{101}}, {12, []byte{}}},
	})
}

func BenchmarkMarshalMessage(b *testing.B) {
	options := make(coap.Options, 0, 32)
	bufOptions := make([]byte, 1024)
	bufOptionsUsed := bufOptions

	enc := 0

	options, enc, _ = options.SetPath(bufOptionsUsed, "/a/b/c/d/e")
	bufOptionsUsed = bufOptionsUsed[enc:]

	options, enc, _ = options.SetContentFormat(bufOptionsUsed, coap.TextPlain)
	bufOptionsUsed = bufOptionsUsed[enc:]
	msg := Message{
		Code:    codes.GET,
		Payload: []byte{0x1},
		Token:   []byte{0x1, 0x2, 0x3},
		Options: options,
	}
	buffer := make([]byte, 1024)

	b.ResetTimer()
	for i := uint32(0); i < uint32(b.N); i++ {

		_, err := msg.MarshalTo(buffer)
		if err != nil {
			b.Fatalf("cannot marshal")
		}
	}
}

func BenchmarkUnmarshalMessage(b *testing.B) {
	buffer := []byte{211, 0, 1, 1, 2, 3, 177, 97, 1, 98, 1, 99, 1, 100, 1, 101, 16, 255, 1}
	options := make(coap.Options, 0, 32)
	msg := Message{
		Options: options,
	}

	b.ResetTimer()
	for i := uint32(0); i < uint32(b.N); i++ {
		msg.Options = options
		_, err := msg.Unmarshal(buffer)
		if err != nil {
			b.Fatalf("cannot unmarshal: %v", err)
		}
	}
}