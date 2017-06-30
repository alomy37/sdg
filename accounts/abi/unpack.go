// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package abi

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
)

<<<<<<< HEAD
// toGoSliceType parses the input and casts it to the proper slice defined by the ABI
// argument in T.
func toGoSlice(i int, t Argument, output []byte) (interface{}, error) {
	index := i * 32
	// The slice must, at very least be large enough for the index+32 which is exactly the size required
	// for the [offset in output, size of offset].
	if index+32 > len(output) {
		return nil, fmt.Errorf("abi: cannot marshal in to go slice: insufficient size output %d require %d", len(output), index+32)
	}

	elem := t.Type.Elem

	// this value will become our slice or our array, depending on the type
	var refSlice reflect.Value
	var slice []byte
	var size int
	var offset int
	if t.Type.IsSlice {
		// get the offset which determines the start of this array ...
		offset = int(binary.BigEndian.Uint64(output[index+24 : index+32]))
		if offset+32 > len(output) {
			return nil, fmt.Errorf("abi: cannot marshal in to go slice: offset %d would go over slice boundary (len=%d)", len(output), offset+32)
		}

		slice = output[offset:]
		// ... starting with the size of the array in elements ...
		size = int(binary.BigEndian.Uint64(slice[24:32]))
		slice = slice[32:]
		// ... and make sure that we've at the very least the amount of bytes
		// available in the buffer.
		if size*32 > len(slice) {
			return nil, fmt.Errorf("abi: cannot marshal in to go slice: insufficient size output %d require %d", len(output), offset+32+size*32)
		}

		// reslice to match the required size
		slice = slice[:size*32]
		// declare our slice
		refSlice = reflect.MakeSlice(reflect.SliceOf(elem.Type), size, size)
	} else if t.Type.T == ArrayTy {
		//get the number of elements in the array
		size = t.Type.Size
		// declare our slice
		refSlice = reflect.New(reflect.ArrayOf(size, elem.Type)).Elem()
		//check to make sure array size matches up
		if index+32*size > len(output) {
			return nil, fmt.Errorf("abi: cannot marshal in to go array: offset %d would go over slice boundary (len=%d)", len(output), index+32*size)
		}
		//slice is there for a fixed amount of times
		slice = output[index : index+size*32]
	}

	for i := 0; i < size; i++ {
		var (
			inter        interface{}             // interface type
			returnOutput = slice[i*32 : i*32+32] // the return output
			err          error
		)
		// set inter to the correct type (cast)
		switch elem.T {
		case IntTy, UintTy:
			inter = readInteger(elem.Kind, returnOutput)
		case BoolTy:
			inter, err = readBool(returnOutput)
			if err != nil {
				return nil, err
			}
		case AddressTy:
			inter = common.BytesToAddress(returnOutput)
		case HashTy:
			inter = common.BytesToHash(returnOutput)
		case FixedBytesTy:
			inter = returnOutput
		case SliceTy, ArrayTy:
			fmt.Println("Index: ", i+index/32)
			/*inter, err = toGoSlice(i+index/32, t, output)
			if err != nil {
				return nil, err
			}*/
		default:
			return nil, fmt.Errorf("abi: unsupported slice type passed in")
		}

		//fmt.Printf("type: %T, value: %v\n", inter, inter)
		//fmt.Printf("%v\n", elem.stringKind)
		// append the item to our reflect slice
		refSlice.Index(i).Set(reflect.ValueOf(inter))
	}

	// return the interface
	return refSlice.Interface(), nil
=======
type unpacker interface {
	tupleUnpack(v interface{}, output []byte) error
	singleUnpack(v interface{}, output []byte) error
	tupleReturn() bool
>>>>>>> 03ed394... accounts/abi: redo unpacking logic into nice modular pieces
}

func readInteger(kind reflect.Kind, b []byte) interface{} {
	switch kind {
	case reflect.Uint8:
		return uint8(b[len(b)-1])
	case reflect.Uint16:
		return binary.BigEndian.Uint16(b[len(b)-2:])
	case reflect.Uint32:
		return binary.BigEndian.Uint32(b[len(b)-4:])
	case reflect.Uint64:
		return binary.BigEndian.Uint64(b[len(b)-8:])
	case reflect.Int8:
		return int8(b[len(b)-1])
	case reflect.Int16:
		return int16(binary.BigEndian.Uint16(b[len(b)-2:]))
	case reflect.Int32:
		return int32(binary.BigEndian.Uint32(b[len(b)-4:]))
	case reflect.Int64:
		return int64(binary.BigEndian.Uint64(b[len(b)-8:]))
	default:
		return new(big.Int).SetBytes(b)
	}
}

func readBool(word []byte) (bool, error) {
	if len(word) != 32 {
		return false, fmt.Errorf("abi: fatal error: incorrect word length")
	}

	for i, b := range word {
		if b != 0 && i != 31 {
			return false, errBadBool
		}
	}
	switch word[31] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errBadBool
	}
}

func readFunctionType(t Type, word []byte) (funcTy [24]byte, err error) {
	if t.T != FunctionTy {
		return [24]byte{}, fmt.Errorf("abi: invalid type in call to make function type byte array.")
	}
	if garbage := binary.BigEndian.Uint64(word[24:32]); garbage != 0 {
		err = fmt.Errorf("abi: got improperly encoded function type, got %v", word)
	} else {
		copy(funcTy[:], word[0:24])
	}
	return
}

<<<<<<< HEAD
// toGoType parses the input and casts it to the proper type defined by the ABI
// argument in T.
func toGoType(i int, t Argument, output []byte) (interface{}, error) {
	// we need to treat slices differently
	if (t.Type.IsSlice || t.Type.IsArray) && t.Type.T != BytesTy && t.Type.T != StringTy && t.Type.T != FixedBytesTy && t.Type.T != FunctionTy {
		return toGoSlice(i, t, output)
=======
func readFixedBytes(t Type, word []byte) (interface{}, error) {
	if t.T != FixedBytesTy {
		return nil, fmt.Errorf("abi: invalid type in call to make fixed byte array.")
>>>>>>> 03ed394... accounts/abi: redo unpacking logic into nice modular pieces
	}
	// convert
	array := reflect.New(t.Type).Elem()

	reflect.Copy(array, reflect.ValueOf(word[0:t.Size]))
	return array, nil

}

func forEachUnpack(t Type, output []byte, start, size int) (interface{}, error) {
	if start+32*size > len(output) {
		return nil, fmt.Errorf("abi: cannot marshal in to go array: offset %d would go over slice boundary (len=%d)", len(output), start+32*size)
	}

	// this value will become our slice or our array, depending on the type
	var refSlice reflect.Value
	slice := output[start : size*32]
	if t.T == SliceTy {
		// declare our slice
		refSlice = reflect.MakeSlice(t.Type, size, size)
	} else if t.T == ArrayTy {
		// declare our array
		refSlice = reflect.New(t.Type).Elem()
	} else {
		return nil, fmt.Errorf("abi: invalid type in array/slice unpacking stage")
	}

	for i, j := start, 0; j*32 < len(slice); i, j = i+32, j+1 {
		inter, err := toGoType(i, *t.Elem, output)
		if err != nil {
			return nil, err
		}
		// append the item to our reflect slice
		refSlice.Index(j).Set(reflect.ValueOf(inter))
	}

	// return the interface
	return refSlice.Interface(), nil
}

// toGoType parses the input and casts it to the proper type defined by the ABI
// argument in T.
func toGoType(index int, t Type, output []byte) (interface{}, error) {
	if index+32 > len(output) {
		return nil, fmt.Errorf("abi: cannot marshal in to go type: length insufficient %d require %d", len(output), index+32)
	}

	// Parse the given index output and check whether we need to read
	// a different offset and length based on the type (i.e. string, bytes)
	var (
		returnOutput []byte
		i, j         int
		err          error
	)

	if t.requiresLengthPrefix() {
		i, j, err = lengthPrefixPointsTo(index, output)
		if err != nil {
			return nil, err
		}
	} else {
		returnOutput = output[index : index+32]
	}
	switch t.T {
	case SliceTy:
		return forEachUnpack(t, output, i, j)
	case ArrayTy:
		return forEachUnpack(t, output, i, t.Size)
	case StringTy: // variable arrays are written at the end of the return bytes
		return string(output[i : i+j]), nil
	case IntTy, UintTy:
		return readInteger(t.Kind, returnOutput), nil
	case BoolTy:
		return readBool(returnOutput)
	case AddressTy:
		return common.BytesToAddress(returnOutput), nil
	case HashTy:
		return common.BytesToHash(returnOutput), nil
	case BytesTy:
		return returnOutput, nil
	case FixedBytesTy:
		return readFixedBytes(t, returnOutput)
	case FunctionTy:
		return readFunctionType(t, returnOutput)
	default:
		return nil, fmt.Errorf("abi: unknown type %v", t.T)
	}
}

// interprets a 32 byte slice as an offset and then determines which indice to look to decode the type.
func lengthPrefixPointsTo(index int, output []byte) (start int, length int, err error) {
	offset := int(binary.BigEndian.Uint64(output[index+24 : index+32]))
	if offset+32 > len(output) {
		return 0, 0, fmt.Errorf("abi: cannot marshal in to go slice: offset %d would go over slice boundary (len=%d)", len(output), offset+32)
	}
	length = int(binary.BigEndian.Uint64(output[offset+24 : offset+32]))
	if offset+32+length > len(output) {
		return 0, 0, fmt.Errorf("abi: cannot marshal in to go type: length insufficient %d require %d", len(output), offset+32+length)
	}
	start = offset + 32
	return
}

func bytesAreProper(output []byte) error {
	if len(output) == 0 {
		return fmt.Errorf("abi: unmarshalling empty output")
	} else if len(output)%32 != 0 {
		return fmt.Errorf("abi: improperly formatted output")
	} else {
		return nil
	}
}
