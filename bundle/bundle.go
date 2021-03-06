package bundle

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/ugorji/go/codec"
)

// Bundle represents a bundle as defined in section 4.2.1. Each Bundle contains
// one primary block and multiple canonical blocks.
type Bundle struct {
	PrimaryBlock    PrimaryBlock
	CanonicalBlocks []CanonicalBlock
}

// NewBundle creates a new Bundle. The values and flags of the blocks will be
// checked and an error might be returned.
func NewBundle(primary PrimaryBlock, canonicals []CanonicalBlock) (b Bundle, err error) {
	b = Bundle{
		PrimaryBlock:    primary,
		CanonicalBlocks: canonicals,
	}
	err = b.checkValid()

	return
}

// forEachBlock applies the given function for each of this Bundle's blocks.
func (b *Bundle) forEachBlock(f func(block)) {
	f(&b.PrimaryBlock)
	for i := 0; i < len(b.CanonicalBlocks); i++ {
		f(&b.CanonicalBlocks[i])
	}
}

// ExtensionBlock returns this Bundle's canonical block/extension block
// matching the requested block type code. If no such block was found,
// an error will be returned.
func (b *Bundle) ExtensionBlock(blockType CanonicalBlockType) (*CanonicalBlock, error) {
	for i := 0; i < len(b.CanonicalBlocks); i++ {
		cb := &b.CanonicalBlocks[i]
		if (*cb).BlockType == blockType {
			return cb, nil
		}
	}

	return nil, newBundleError(fmt.Sprintf(
		"No CanonicalBlock with block type %d was found in Bundle", blockType))
}

// PayloadBlock returns this Bundle's payload block or an error, if it does
// not exists.
func (b *Bundle) PayloadBlock() (*CanonicalBlock, error) {
	return b.ExtensionBlock(PayloadBlock)
}

// SetCRCType sets the given CRCType for each block. To also calculate and set
// the CRC value, one should also call the CalculateCRC method.
func (b *Bundle) SetCRCType(crcType CRCType) {
	b.forEachBlock(func(blck block) {
		blck.SetCRCType(crcType)
	})
}

// CalculateCRC calculates and sets the CRC value for each block.
func (b *Bundle) CalculateCRC() {
	b.forEachBlock(func(blck block) {
		blck.CalculateCRC()
	})
}

// ID returns a kind of uniquene representation of this bundle, containing
// the souce node and creation timestamp. If this bundle is a fragment, the
// offset is also present.
func (b Bundle) ID() string {
	var bldr strings.Builder

	fmt.Fprintf(&bldr, "%v-%d-%d",
		b.PrimaryBlock.SourceNode,
		b.PrimaryBlock.CreationTimestamp[0],
		b.PrimaryBlock.CreationTimestamp[1])

	if pb := b.PrimaryBlock; pb.BundleControlFlags.Has(IsFragment) {
		fmt.Fprintf(&bldr, "-%d", pb.FragmentOffset)
	}

	return bldr.String()
}

func (b Bundle) String() string {
	return b.ID()
}

// CheckCRC checks the CRC value of each block and returns false if some
// value does not match. This method changes the block's CRC value temporary
// and is not thread safe.
func (b *Bundle) CheckCRC() bool {
	var flag = true

	b.forEachBlock(func(blck block) {
		if !blck.CheckCRC() {
			flag = false
		}
	})

	return flag
}

func (b Bundle) checkValid() (errs error) {
	// Check blocks for errors
	b.forEachBlock(func(blck block) {
		if blckErr := blck.checkValid(); blckErr != nil {
			errs = multierror.Append(errs, blckErr)
		}
	})

	// Check CanonicalBlocks for errors
	if b.PrimaryBlock.BundleControlFlags.Has(AdministrativeRecordPayload) ||
		b.PrimaryBlock.SourceNode == DtnNone() {
		for _, cb := range b.CanonicalBlocks {
			if cb.BlockControlFlags.Has(StatusReportBlock) {
				errs = multierror.Append(errs,
					newBundleError("Bundle: Bundle Processing Control Flags indicate that "+
						"this bundle's payload is an administrative record or the source "+
						"node is omitted, but the \"Transmit status report if block canot "+
						"be processed\" Block Processing Control Flag was set in a "+
						"Canonical Block"))
			}
		}
	}

	// Check uniqueness of block numbers
	var cbBlockNumbers = make(map[uint]bool)
	// Check max 1 occurrence of extension blocks
	var cbBlockTypes = make(map[CanonicalBlockType]bool)

	for _, cb := range b.CanonicalBlocks {
		if _, ok := cbBlockNumbers[cb.BlockNumber]; ok {
			errs = multierror.Append(errs,
				newBundleError(fmt.Sprintf(
					"Bundle: Block number %d occurred multiple times", cb.BlockNumber)))
		}
		cbBlockNumbers[cb.BlockNumber] = true

		switch cb.BlockType {
		case PreviousNodeBlock, BundleAgeBlock, HopCountBlock:
			if _, ok := cbBlockTypes[cb.BlockType]; ok {
				errs = multierror.Append(errs,
					newBundleError(fmt.Sprintf(
						"Bundle: Block type %d occurred multiple times", cb.BlockType)))
			}
			cbBlockTypes[cb.BlockType] = true
		}
	}

	if b.PrimaryBlock.CreationTimestamp[0] == 0 {
		if _, ok := cbBlockTypes[BundleAgeBlock]; !ok {
			errs = multierror.Append(errs, newBundleError(
				"Bundle: Creation Timestamp is zero, but no Bundle Age block is present"))
		}
	}

	return
}

// IsAdministrativeRecord returns if this Bundle's control flags indicate this
// has an administrative record payload.
func (b Bundle) IsAdministrativeRecord() bool {
	return b.PrimaryBlock.BundleControlFlags.Has(AdministrativeRecordPayload)
}

// ToCbor creates a byte array representing a CBOR indefinite-length array of
// this Bundle with all its blocks, as defined in section 4 of the Bundle
// Protocol Version 7.
func (b Bundle) ToCbor() []byte {
	// It seems to be tricky using both definite-length and indefinite-length
	// arays with the codec library. However, an indefinite-length array is just
	// a byte array wrapped between the start and "break" code, which are
	// exported as consts from the codec library.

	var buf bytes.Buffer
	var cborHandle *codec.CborHandle = new(codec.CborHandle)

	buf.WriteByte(codec.CborStreamArray)

	b.forEachBlock(func(blck block) {
		codec.NewEncoder(&buf, cborHandle).MustEncode(blck)
	})

	buf.WriteByte(codec.CborStreamBreak)

	return buf.Bytes()
}

// decodeBundleBlock decodes an already generic decoded block to its
// determinated data structure.
// The NewBundleFromCbor function decodes an array of interface{} which results
// in an array of arrays, as codec tries to decode the whole data. This method
// will re-encode this "anonymous" array to CBOR and will decode it to its
// struct, which is referenced as the target pointer.
func decodeBundleBlock(data interface{}, target interface{}) {
	var b []byte = make([]byte, 0, 64)
	var cborHandle *codec.CborHandle = new(codec.CborHandle)

	codec.NewEncoderBytes(&b, cborHandle).MustEncode(data)
	codec.NewDecoderBytes(b, cborHandle).MustDecode(target)
}

// NewBundleFromCbor tries to decodes the given data from CBOR into a Bundle.
// It also checks the whole bundle's validity and each block's CRC value.
func NewBundleFromCbor(data []byte) (b Bundle, err error) {
	// The decoding might panic and would be recovered in the following function,
	// which returns an error.
	defer func() {
		if r := recover(); r != nil {
			err = newBundleError(fmt.Sprintf("Bundle: Decoding CBOR failed, %v", r))
		}
	}()

	var dataArr []interface{}
	codec.NewDecoderBytes(data, new(codec.CborHandle)).MustDecode(&dataArr)

	var pb PrimaryBlock
	decodeBundleBlock(dataArr[0], &pb)

	var cb []CanonicalBlock = make([]CanonicalBlock, len(dataArr)-1)
	for i := 0; i < len(cb); i++ {
		decodeBundleBlock(dataArr[i+1], &cb[i])
	}

	b = Bundle{pb, cb}

	if chkVldErr := b.checkValid(); chkVldErr != nil {
		err = multierror.Append(err, chkVldErr)
	}

	if !b.CheckCRC() {
		err = multierror.Append(err, newBundleError("CRC failed"))
	}

	return
}
