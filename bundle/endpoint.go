package bundle

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/ugorji/go/codec"
)

const (
	endpointURISchemeDTN uint = 1
	endpointURISchemeIPN uint = 2
)

// EndpointID represents an Endpoint ID as defined in section 4.1.5.1. The
// "scheme name" is represented by an uint and the "scheme-specific part"
// (SSP) by an interface{}. Based on the characteristic of the name, the
// underlying type of the SSP may vary.
type EndpointID struct {
	SchemeName         uint
	SchemeSpecificPart interface{}
}

func newEndpointIDDTN(ssp string) (EndpointID, error) {
	var sspRaw interface{}
	if ssp == "none" {
		sspRaw = uint(0)
	} else {
		sspRaw = string(ssp)
	}

	return EndpointID{
		SchemeName:         endpointURISchemeDTN,
		SchemeSpecificPart: sspRaw,
	}, nil
}

func newEndpointIDIPN(ssp string) (ep EndpointID, err error) {
	// As definied in RFC 6260, section 2.1:
	// - node number: ASCII numeric digits between 1 and (2^64-1)
	// - an ASCII dot
	// - service number: ASCII numeric digits between 1 and (2^64-1)

	re := regexp.MustCompile(`^(\d+)\.(\d+)$`)
	matches := re.FindStringSubmatch(ssp)
	if len(matches) != 3 {
		err = newBundleError("IPN does not satisfy given regex")
		return
	}

	nodeNo, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return
	}

	serviceNo, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return
	}

	if nodeNo < 1 || serviceNo < 1 {
		err = newBundleError("IPN's node and service number must be >= 1")
		return
	}

	ep = EndpointID{
		SchemeName:         endpointURISchemeIPN,
		SchemeSpecificPart: [2]uint64{nodeNo, serviceNo},
	}
	return
}

// NewEndpointID creates a new EndpointID by a given "scheme name" and a
// "scheme-specific part" (SSP). Currently the "dtn" and "ipn"-scheme names
// are supported.
//
// Example: "dtn:foobar"
func NewEndpointID(eid string) (e EndpointID, err error) {
	re := regexp.MustCompile(`^([[:alnum:]]+):(.+)$`)
	matches := re.FindStringSubmatch(eid)

	if len(matches) != 3 {
		err = newBundleError("eid does not satisfy regex")
		return
	}

	name := matches[1]
	ssp := matches[2]

	switch name {
	case "dtn":
		return newEndpointIDDTN(ssp)
	case "ipn":
		return newEndpointIDIPN(ssp)
	default:
		return EndpointID{}, newBundleError("Unknown scheme type")
	}
}

// MustNewEndpointID returns a new EndpointID like NewEndpointID, but panics
// in case of an error.
func MustNewEndpointID(eid string) EndpointID {
	ep, err := NewEndpointID(eid)
	if err != nil {
		panic(err)
	}

	return ep
}

// setEndpointIDFromCborArray sets the fields of the EndpointID addressed by
// the EndpointID-pointer based on the given array. This function is used for
// the CBOR decoding of the PrimaryBlock and some Extension Blocks.
func setEndpointIDFromCborArray(ep *EndpointID, arr []interface{}) {
	(*ep).SchemeName = uint(arr[0].(uint64))
	(*ep).SchemeSpecificPart = arr[1]

	// The codec library uses uint64 for uints and []interface{} for arrays
	// internally. However, our `dtn:none` is defined by an uint and each "ipn"
	// endpoint by an uint64 array. That's why we have to re-cast some types..

	switch ty := reflect.TypeOf((*ep).SchemeSpecificPart); ty.Kind() {
	case reflect.Uint64:
		(*ep).SchemeSpecificPart = uint((*ep).SchemeSpecificPart.(uint64))

	case reflect.Slice:
		(*ep).SchemeSpecificPart = [2]uint64{
			(*ep).SchemeSpecificPart.([]interface{})[0].(uint64),
			(*ep).SchemeSpecificPart.([]interface{})[1].(uint64),
		}
	}
}

func (eid *EndpointID) CodecEncodeSelf(enc *codec.Encoder) {
	var blockArr = []interface{}{
		eid.SchemeName,
		eid.SchemeSpecificPart,
	}

	enc.MustEncode(blockArr)
}

func (eid *EndpointID) CodecDecodeSelf(dec *codec.Decoder) {
	var blockArrPt = new([]interface{})
	dec.MustDecode(blockArrPt)

	setEndpointIDFromCborArray(eid, *blockArrPt)
}

func (eid EndpointID) checkValidDtn() error {
	switch eid.SchemeSpecificPart.(type) {
	case string:
		if eid.SchemeSpecificPart.(string) == "none" {
			return newBundleError("EndpointID: equals dtn:none, with none as a string")
		}
	}

	return nil
}

func (eid EndpointID) checkValidIpn() error {
	ssp := eid.SchemeSpecificPart.([2]uint64)
	if ssp[0] < 1 || ssp[1] < 1 {
		return newBundleError("EndpointID: IPN's node and service number must be >= 1")
	}

	return nil
}

func (eid EndpointID) checkValid() error {
	switch eid.SchemeName {
	case endpointURISchemeDTN:
		return eid.checkValidDtn()

	case endpointURISchemeIPN:
		return eid.checkValidIpn()

	default:
		return newBundleError("EndpointID: unknown scheme name")
	}
}

func (eid EndpointID) String() string {
	var b strings.Builder

	switch eid.SchemeName {
	case endpointURISchemeDTN:
		b.WriteString("dtn")
	case endpointURISchemeIPN:
		b.WriteString("ipn")
	default:
		fmt.Fprintf(&b, "unknown_%d", eid.SchemeName)
	}
	b.WriteRune(':')

	switch t := eid.SchemeSpecificPart.(type) {
	case uint:
		if eid.SchemeName == endpointURISchemeDTN && eid.SchemeSpecificPart.(uint) == 0 {
			b.WriteString("none")
		} else {
			fmt.Fprintf(&b, "%d", eid.SchemeSpecificPart.(uint))
		}

	case string:
		b.WriteString(eid.SchemeSpecificPart.(string))

	case [2]uint64:
		var ssp [2]uint64 = eid.SchemeSpecificPart.([2]uint64)
		if eid.SchemeName == endpointURISchemeIPN {
			fmt.Fprintf(&b, "%d.%d", ssp[0], ssp[1])
		} else {
			fmt.Fprintf(&b, "%v", ssp)
		}

	default:
		fmt.Fprintf(&b, "unknown %T: %v", t, eid.SchemeSpecificPart)
	}

	return b.String()
}

// DtnNone returns the "null endpoint", "dtn:none".
func DtnNone() EndpointID {
	return EndpointID{
		SchemeName:         endpointURISchemeDTN,
		SchemeSpecificPart: uint(0),
	}
}
