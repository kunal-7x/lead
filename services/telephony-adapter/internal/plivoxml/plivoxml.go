// Package plivoxml builds Plivo XML (PXML) responses for Answer/Hangup URLs.
//
// Plivo fetches the Answer URL when a call is answered and expects an XML
// document describing what to do next (Speak, Play, Dial, GetDigits, etc.).
// See https://www.plivo.com/docs/voice/xml/getting-started/ for the schema.
package plivoxml

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// Response is the root <Response> element returned to Plivo.
type Response struct {
	XMLName  xml.Name `xml:"Response"`
	Elements []any    `xml:",any"`
}

// Add appends a child element.
func (r *Response) Add(e any) { r.Elements = append(r.Elements, e) }

// Encode returns the XML body suitable for an HTTP response.
// It always includes the XML prolog and a trailing newline.
func (r *Response) Encode() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, fmt.Errorf("plivoxml: encode: %w", err)
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// Speak is text-to-speech using Plivo's built-in TTS.
type Speak struct {
	XMLName  xml.Name `xml:"Speak"`
	Voice    string   `xml:"voice,attr,omitempty"`
	Language string   `xml:"language,attr,omitempty"`
	Text     string   `xml:",chardata"`
}

// Play plays a remote audio file (mp3/wav).
type Play struct {
	XMLName xml.Name `xml:"Play"`
	Loop    int      `xml:"loop,attr,omitempty"`
	URL     string   `xml:",chardata"`
}

// Hangup ends the call.
type Hangup struct {
	XMLName xml.Name `xml:"Hangup"`
	Reason  string   `xml:"reason,attr,omitempty"`
}

// Record records the caller's audio and POSTs the URL to action.
type Record struct {
	XMLName       xml.Name `xml:"Record"`
	Action        string   `xml:"action,attr,omitempty"`
	Method        string   `xml:"method,attr,omitempty"`
	MaxLength     int      `xml:"maxLength,attr,omitempty"`
	FinishOnKey   string   `xml:"finishOnKey,attr,omitempty"`
	PlayBeep      bool     `xml:"playBeep,attr,omitempty"`
	RecordSession bool     `xml:"recordSession,attr,omitempty"`
}

// GetDigits collects DTMF input and posts to action.
type GetDigits struct {
	XMLName     xml.Name `xml:"GetDigits"`
	Action      string   `xml:"action,attr,omitempty"`
	Method      string   `xml:"method,attr,omitempty"`
	NumDigits   int      `xml:"numDigits,attr,omitempty"`
	Timeout     int      `xml:"timeout,attr,omitempty"`
	FinishOnKey string   `xml:"finishOnKey,attr,omitempty"`
	Inner       []any    `xml:",any"`
}

// Dial bridges the current call to one or more Number/SIP endpoints.
type Dial struct {
	XMLName  xml.Name `xml:"Dial"`
	Action   string   `xml:"action,attr,omitempty"`
	Method   string   `xml:"method,attr,omitempty"`
	CallerID string   `xml:"callerId,attr,omitempty"`
	Record   string   `xml:"record,attr,omitempty"`
	TimeLimit int     `xml:"timeLimit,attr,omitempty"`
	Inner    []any    `xml:",any"`
}

// SIPEndpoint is a child of <Dial> that bridges to a SIP URI.
type SIPEndpoint struct {
	XMLName xml.Name `xml:"User"`
	URI     string   `xml:",chardata"`
}

// PhoneNumber is a child of <Dial> that bridges to a PSTN number.
type PhoneNumber struct {
	XMLName xml.Name `xml:"Number"`
	Number  string   `xml:",chardata"`
}

// SmokeResponse builds the C7 smoke-test XML body:
//   <Response><Speak>Hello, this is Capsy testing.</Speak></Response>
func SmokeResponse() ([]byte, error) {
	r := &Response{}
	r.Add(Speak{Voice: "Polly.Aditi", Language: "en-IN", Text: "Hello, this is Capsy testing."})
	return r.Encode()
}

// BridgeToSIP builds an Answer URL XML that <Dial>s the given SIP URI
// (used to hand the call off to FreeSWITCH once C12 lands).
func BridgeToSIP(sipURI, callerID string) ([]byte, error) {
	r := &Response{}
	d := Dial{CallerID: callerID, TimeLimit: 1800}
	d.Inner = append(d.Inner, SIPEndpoint{URI: sipURI})
	r.Add(d)
	return r.Encode()
}
