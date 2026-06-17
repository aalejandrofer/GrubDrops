package twitch

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Pickle opcodes (subset needed for SimpleCookie parsing)
const (
	opMARK        = '('
	opSTOP        = '.'
	opPOP         = '0'
	opPOP_MARK    = '1'
	opDUP         = '2'
	opFLOAT       = 'F'
	opINT         = 'I'
	opBININT      = 'J'
	opBININT1     = 'K'
	opBININT2     = 'M'
	opLONG        = 'L'
	opLONG1       = 0x8a
	opLONG4       = 0x8b
	opSTRING      = 'S'
	opSTRING1     = 'U' // SHORT_BINSTRING
	opSTRING4     = 0x8d
	opUNICODE     = 'V'
	opUNICODE1    = 'X' // SHORT_BINUNICODE
	opUNICODE4    = 0x8e
	opBINUNICODE  = 0x8c // BINUNICODE (protocol 4)
	opEMPTY_TUPLE = ')'
	opTUPLE1      = 0x85
	opTUPLE2      = 0x86
	opTUPLE3      = 0x87
	opEMPTY_LIST  = ']'
	opAPPEND      = 'a'
	opAPPENDS     = 'e'
	opEMPTY_DICT  = '}'
	opSETITEM     = 's'
	opSETITEMS    = 'u'
	opGLOBAL      = 'c'
	opINST        = 'i'
	opREDUCE      = 'R'
	opBUILD      = 'b'
	opNEWOBJ      = 0x81
	opOBJ         = 'o'
	opPUT         = 'p'
	opGET         = 'g'
	opBINGET      = 'h'
	opLONG_BINGET = 'j'
	opBINPUT      = 'q'
	opLONG_BINPUT = 'r'
	opPROTO       = 0x80
	opNEWFALSE    = 0x88
	opNEWTRUE     = 0x89
	opNEWOBJ_EX   = 0x92
	opSTACK_GLOBAL = 0x93
	opMEMOIZE     = 0x94
	opFRAME       = 0x95
)

// pickleReader reads pickle protocol data
type pickleReader struct {
	data   []byte
	pos    int
	stack  []interface{}
	marks  []int
	memo   map[int]interface{}
}

func newPickleReader(data []byte) *pickleReader {
	return &pickleReader{
		data: data,
		pos:  0,
		memo: make(map[int]interface{}),
	}
}

func (r *pickleReader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *pickleReader) readBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, io.EOF
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *pickleReader) readLine() ([]byte, error) {
	idx := bytes.IndexByte(r.data[r.pos:], '\n')
	if idx < 0 {
		idx = len(r.data) - r.pos
	} else {
		idx++ // include the newline
	}
	line := r.data[r.pos : r.pos+idx]
	r.pos += idx
	return line, nil
}

func (r *pickleReader) push(v interface{}) {
	r.stack = append(r.stack, v)
}

func (r *pickleReader) pop() (interface{}, error) {
	if len(r.stack) == 0 {
		return nil, errors.New("pickle: stack underflow")
	}
	v := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	return v, nil
}

func (r *pickleReader) popMark() ([]interface{}, error) {
	if len(r.marks) == 0 {
		return nil, errors.New("pickle: mark underflow")
	}
	markIdx := r.marks[len(r.marks)-1]
	r.marks = r.marks[:len(r.marks)-1]
	if markIdx > len(r.stack) {
		return nil, errors.New("pickle: invalid mark index")
	}
	items := r.stack[markIdx:]
	r.stack = r.stack[:markIdx]
	return items, nil
}

func (r *pickleReader) peek() (interface{}, error) {
	if len(r.stack) == 0 {
		return nil, errors.New("pickle: stack empty")
	}
	return r.stack[len(r.stack)-1], nil
}

// ParsePickleCookies parses a Python pickle file containing SimpleCookie
// Returns a map of cookie name -> value
// Supports both dict and defaultdict structures
func ParsePickleCookies(data []byte) (map[string]string, error) {
	r := newPickleReader(data)

	for r.pos < len(r.data) {
		op, err := r.readByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		switch op {
		case opPROTO:
			// Protocol version byte
			_, _ = r.readByte()

		case opMARK:
			r.marks = append(r.marks, len(r.stack))

		case opSTOP:
			// Done parsing
			goto done

		case opPOP:
			_, _ = r.pop()

		case opPOP_MARK:
			_, _ = r.popMark()

		case opDUP:
			v, err := r.peek()
			if err != nil {
				return nil, err
			}
			r.push(v)

		case opINT:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			s := strings.TrimSpace(string(line))
			if s == "01" {
				r.push(true)
			} else if s == "00" {
				r.push(false)
			} else {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("pickle: invalid INT: %s", s)
				}
				r.push(n)
			}

		case opBININT:
			b, err := r.readBytes(4)
			if err != nil {
				return nil, err
			}
			n := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16 | int32(b[3])<<24
			r.push(n)

		case opBININT1:
			b, err := r.readByte()
			if err != nil {
				return nil, err
			}
			r.push(int32(b))

		case opBININT2:
			b, err := r.readBytes(2)
			if err != nil {
				return nil, err
			}
			n := int32(b[0]) | int32(b[1])<<8
			r.push(n)

		case opLONG:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			s := strings.TrimSpace(string(line))
			s = strings.TrimSuffix(s, "L")
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("pickle: invalid LONG: %s", s)
			}
			r.push(n)

		case opLONG1:
			n, err := r.readByte()
			if err != nil {
				return nil, err
			}
			b, err := r.readBytes(int(n))
			if err != nil {
				return nil, err
			}
			v := decodeLong(b)
			r.push(v)

		case opLONG4:
			b4, err := r.readBytes(4)
			if err != nil {
				return nil, err
			}
			n := int(b4[0]) | int(b4[1])<<8 | int(b4[2])<<16 | int(b4[3])<<24
			b, err := r.readBytes(n)
			if err != nil {
				return nil, err
			}
			v := decodeLong(b)
			r.push(v)

		case opFLOAT:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			s := strings.TrimSpace(string(line))
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("pickle: invalid FLOAT: %s", s)
			}
			r.push(f)

		case opSTRING:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			s := string(line)
			// Remove quotes
			if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
				s = s[1 : len(s)-1]
			}
			r.push(s)

		case opSTRING1:
			n, err := r.readByte()
			if err != nil {
				return nil, err
			}
			b, err := r.readBytes(int(n))
			if err != nil {
				return nil, err
			}
			r.push(string(b))

		case opSTRING4:
			b4, err := r.readBytes(4)
			if err != nil {
				return nil, err
			}
			n := int(b4[0]) | int(b4[1])<<8 | int(b4[2])<<16 | int(b4[3])<<24
			b, err := r.readBytes(n)
			if err != nil {
				return nil, err
			}
			r.push(string(b))

		case opUNICODE:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			s := strings.TrimSuffix(string(line), "\n")
			r.push(s)

		case opUNICODE1:
			n, err := r.readByte()
			if err != nil {
				return nil, err
			}
			b, err := r.readBytes(int(n))
			if err != nil {
				return nil, err
			}
			r.push(string(b))

		case opUNICODE4:
			b4, err := r.readBytes(4)
			if err != nil {
				return nil, err
			}
			n := int(b4[0]) | int(b4[1])<<8 | int(b4[2])<<16 | int(b4[3])<<24
			b, err := r.readBytes(n)
			if err != nil {
				return nil, err
			}
			r.push(string(b))

		case opBINUNICODE:
			// SHORT_BINUNICODE (protocol 4+): 1-byte length prefix
			// Note: Despite the name BINUNICODE, this is actually SHORT_BINUNICODE
			// in protocol 4+, which uses 1-byte length (like STRING1)
			n, err := r.readByte()
			if err != nil {
				return nil, err
			}
			b, err := r.readBytes(int(n))
			if err != nil {
				return nil, err
			}
			r.push(string(b))

		case opEMPTY_TUPLE:
			r.push([]interface{}{})

		case opTUPLE1:
			v, err := r.pop()
			if err != nil {
				return nil, err
			}
			r.push([]interface{}{v})

		case opTUPLE2:
			v2, err := r.pop()
			if err != nil {
				return nil, err
			}
			v1, err := r.pop()
			if err != nil {
				return nil, err
			}
			r.push([]interface{}{v1, v2})

		case opTUPLE3:
			v3, err := r.pop()
			if err != nil {
				return nil, err
			}
			v2, err := r.pop()
			if err != nil {
				return nil, err
			}
			v1, err := r.pop()
			if err != nil {
				return nil, err
			}
			r.push([]interface{}{v1, v2, v3})

		case opEMPTY_LIST:
			r.push([]interface{}{})

		case opAPPEND:
			v, err := r.pop()
			if err != nil {
				return nil, err
			}
			list, err := r.peek()
			if err != nil {
				return nil, err
			}
			if lst, ok := list.([]interface{}); ok {
				r.stack[len(r.stack)-1] = append(lst, v)
			} else {
				return nil, errors.New("pickle: APPEND to non-list")
			}

		case opAPPENDS:
			items, err := r.popMark()
			if err != nil {
				return nil, err
			}
			list, err := r.peek()
			if err != nil {
				return nil, err
			}
			if lst, ok := list.([]interface{}); ok {
				r.stack[len(r.stack)-1] = append(lst, items...)
			} else {
				return nil, errors.New("pickle: APPENDS to non-list")
			}

		case opEMPTY_DICT:
			r.push(map[string]interface{}{})

		case opSETITEM:
			v, err := r.pop()
			if err != nil {
				return nil, err
			}
			k, err := r.pop()
			if err != nil {
				return nil, err
			}
			dict, err := r.peek()
			if err != nil {
				return nil, err
			}
			if d, ok := dict.(map[string]interface{}); ok {
				// Convert key to string (handles tuples, etc.)
				key := fmt.Sprintf("%v", k)
				d[key] = v
			} else {
				return nil, errors.New("pickle: SETITEM to non-dict")
			}

		case opSETITEMS:
			items, err := r.popMark()
			if err != nil {
				return nil, err
			}
			if len(items)%2 != 0 {
				return nil, errors.New("pickle: SETITEMS odd number of items")
			}
			dict, err := r.peek()
			if err != nil {
				return nil, err
			}
			if d, ok := dict.(map[string]interface{}); ok {
				for i := 0; i < len(items); i += 2 {
					// Convert key to string (handles tuples, etc.)
					key := fmt.Sprintf("%v", items[i])
					d[key] = items[i+1]
				}
			} else {
				return nil, errors.New("pickle: SETITEMS to non-dict")
			}

		case opGLOBAL:
			// module + name
			_, err := r.readLine() // module
			if err != nil {
				return nil, err
			}
			_, err = r.readLine() // name
			if err != nil {
				return nil, err
			}
			// Push a placeholder for the global
			r.push("__global__")

		case opSTACK_GLOBAL:
			// Similar to GLOBAL but reads from stack
			name, err := r.pop()
			if err != nil {
				return nil, err
			}
			module, err := r.pop()
			if err != nil {
				return nil, err
			}
			_ = module
			_ = name
			r.push("__global__")

		case opINST:
			_, err := r.readLine() // module
			if err != nil {
				return nil, err
			}
			_, err = r.readLine() // name
			if err != nil {
				return nil, err
			}
			args, err := r.popMark()
			if err != nil {
				return nil, err
			}
			r.push(map[string]interface{}{"__inst_args__": args})

		case opREDUCE:
			args, err := r.pop()
			if err != nil {
				return nil, err
			}
			fn, err := r.pop()
			if err != nil {
				return nil, err
			}
			_ = fn
			r.push(map[string]interface{}{"__reduce_args__": args})

		case opBUILD:
			v, err := r.pop()
			if err != nil {
				return nil, err
			}
			obj, err := r.peek()
			if err != nil {
				return nil, err
			}
			// Try to merge state into object
			if d, ok := obj.(map[string]interface{}); ok {
				if state, ok := v.(map[string]interface{}); ok {
					for k, val := range state {
						d[k] = val
					}
				} else {
					d["__state__"] = v
				}
			}

		case opNEWOBJ:
			args, err := r.pop()
			if err != nil {
				return nil, err
			}
			cls, err := r.pop()
			if err != nil {
				return nil, err
			}
			_ = cls
			r.push(map[string]interface{}{"__newobj_args__": args})

		case opOBJ:
			args, err := r.popMark()
			if err != nil {
				return nil, err
			}
			if len(args) > 0 {
				r.push(args[0])
			} else {
				r.push(map[string]interface{}{})
			}

		case opPUT:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			idx, err := strconv.Atoi(strings.TrimSpace(string(line)))
			if err != nil {
				return nil, err
			}
			v, err := r.peek()
			if err != nil {
				return nil, err
			}
			r.memo[idx] = v

		case opGET:
			line, err := r.readLine()
			if err != nil {
				return nil, err
			}
			idx, err := strconv.Atoi(strings.TrimSpace(string(line)))
			if err != nil {
				return nil, err
			}
			v, ok := r.memo[idx]
			if !ok {
				return nil, fmt.Errorf("pickle: GET %d not in memo", idx)
			}
			r.push(v)

		case opBINGET:
			b, err := r.readByte()
			if err != nil {
				return nil, err
			}
			v, ok := r.memo[int(b)]
			if !ok {
				return nil, fmt.Errorf("pickle: BINGET %d not in memo", b)
			}
			r.push(v)

		case opLONG_BINGET:
			b4, err := r.readBytes(4)
			if err != nil {
				return nil, err
			}
			idx := int(b4[0]) | int(b4[1])<<8 | int(b4[2])<<16 | int(b4[3])<<24
			v, ok := r.memo[idx]
			if !ok {
				return nil, fmt.Errorf("pickle: LONG_BINGET %d not in memo", idx)
			}
			r.push(v)

		case opBINPUT:
			b, err := r.readByte()
			if err != nil {
				return nil, err
			}
			v, err := r.peek()
			if err != nil {
				return nil, err
			}
			r.memo[int(b)] = v

		case opLONG_BINPUT:
			b4, err := r.readBytes(4)
			if err != nil {
				return nil, err
			}
			idx := int(b4[0]) | int(b4[1])<<8 | int(b4[2])<<16 | int(b4[3])<<24
			v, err := r.peek()
			if err != nil {
				return nil, err
			}
			r.memo[idx] = v

		case opFRAME:
			// Skip frame length (8 bytes)
			_, _ = r.readBytes(8)

		case opNEWTRUE:
			r.push(true)

		case opNEWFALSE:
			r.push(false)

		case opMEMOIZE:
			// Store top of stack in memo at next available index
			v, err := r.peek()
			if err != nil {
				return nil, err
			}
			r.memo[len(r.memo)] = v

		case opNEWOBJ_EX:
			kwargs, err := r.pop()
			if err != nil {
				return nil, err
			}
			args, err := r.pop()
			if err != nil {
				return nil, err
			}
			cls, err := r.pop()
			if err != nil {
				return nil, err
			}
			_ = cls
			_ = kwargs
			r.push(map[string]interface{}{"__newobj_ex_args__": args})

		default:
			return nil, fmt.Errorf("pickle: unknown opcode 0x%02x", op)
		}
	}

done:
	// Extract cookies from the parsed structure
	if len(r.stack) == 0 {
		return nil, errors.New("pickle: empty stack after parsing")
	}

	result := make(map[string]string)
	root := r.stack[len(r.stack)-1]

	// The structure is: dict[domain_tuple] -> dict[cookie_name] -> morsel_dict
	// or defaultdict[domain_tuple] -> dict[cookie_name] -> morsel_dict
	// Keys are stored as string representation of tuples, e.g., "(twitch.tv, )"
	if domains, ok := root.(map[string]interface{}); ok {
		for domainKey, cookieDict := range domains {
			// Only process twitch.tv domains
			if !strings.Contains(domainKey, "twitch.tv") {
				continue
			}
			if cookies, ok := cookieDict.(map[string]interface{}); ok {
				for name, morsel := range cookies {
					// Try to extract value from morsel
					if m, ok := morsel.(map[string]interface{}); ok {
						// Try "value" key first (pickle stores Morsel attributes as dict)
						if val, ok := m["value"].(string); ok {
							result[name] = val
							continue
						}
						// Try "coded_value" as fallback
						if val, ok := m["coded_value"].(string); ok {
							result[name] = val
							continue
						}
					}
					// If morsel is a string directly (unlikely but handle it)
					if val, ok := morsel.(string); ok {
						result[name] = val
					}
				}
			}
		}
	}

	return result, nil
}

// decodeLong decodes a little-endian byte slice into an int64
func decodeLong(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	var n int64
	for i := len(b) - 1; i >= 0; i-- {
		n = n<<8 | int64(b[i])
	}
	return n
}

// ParsePickleCookiesFromBase64 parses base64-encoded pickle data
func ParsePickleCookiesFromBase64(b64 string) (map[string]string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	return ParsePickleCookies(data)
}
