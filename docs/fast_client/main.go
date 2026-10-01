// nolint
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

var debug bool

type EasyJSONUnmarshaler interface {
	UnmarshalEasyJSON(l *jlexer_Lexer)
}

type Params struct {
	Tour
	Islands []int
	Routes  []Route
	Stamps  []Stamp
}

type Tour struct {
	TourID      string `json:"tour_id"`
	StartIsland int    `json:"start_island"`
	MaxStamps   int    `json:"max_stamps"`
}

type Route struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type RoutesResponse struct {
	Routes []Route `json:"routes"`
}

type Stamp struct {
	StampID  int   `json:"stamp_id"`
	IslandID int   `json:"island_id"`
	Rarity   int   `json:"rarity"`
	Requires []int `json:"requires"`
}

type StampsResponse struct {
	Stamps []Stamp `json:"stamps"`
}

type solveFunc func(p Params) []int

func solve(p Params) []int {
	islands := getAccessibleIslands(p.StartIsland, p.Routes)
	if debug {
		log.Println("islands:", islands)
	}

	tree := buildStampsTree(p.Stamps, islands)
	if debug {
		log.Println("tree:", tree)
	}

	sum, set := chooseStamps(tree, p.MaxStamps+1) // с учетом фиктивного корня
	if debug {
		log.Println("sum:", sum)
		log.Println("set:", set)
	}

	return set[1:] // обрезаем фиктивный корень
}

// chooseStamps выбирает из дерева не более n штампов, так чтобы их сумма Rarity была максимальной
func chooseStamps(tree map[int]*StampNode, n int) (int, []int) {
	if n >= len(tree) {
		return chooseAll(tree)
	}

	var dfs func(id int, n int) ([]int, [][]int)
	dfs = func(id int, n int) ([]int, [][]int) {
		node := tree[id]
		if n == 1 {
			sums := []int{0, node.Rarity}
			sets := [][]int{nil, {id}}
			return sums, sets
		}
		sums := make([]int, n+1)
		sets := make([][]int, n+1)
		sums[1] = node.Rarity
		sets[1] = []int{id}
		size := 2
		for _, child := range node.Children {
			childSums, childSets := dfs(child, n-1)
			if debug {
				log.Printf("child %d: %v %v", child, childSums[1:], childSets[1:])
			}
			for i := size - 1; i >= 1; i-- {
				for j := 1; j < len(childSums); j++ {
					k := i + j
					if k > n {
						break
					}
					size = max(size, k+1)
					sum := sums[i] + childSums[j]
					if sums[k] < sum {
						sums[k] = sum
						sets[k] = sets[k][:0]
						sets[k] = append(sets[k], sets[i]...)
						sets[k] = append(sets[k], childSets[j]...)
					}
				}
			}
		}
		return sums[:size], sets[:size]
	}

	dp, set := dfs(-1, n)
	return dp[n], set[n]
}

func chooseAll(tree map[int]*StampNode) (int, []int) {
	sum := 0
	set := make([]int, 0, len(tree))

	var dfs func(id int)
	dfs = func(id int) {
		node := tree[id]
		sum += node.Rarity
		set = append(set, id)
		for _, child := range node.Children {
			dfs(child)
		}
	}

	dfs(-1)
	return sum, set
}

// getAccessibleIslands возвращает множество островов доступных со стартового по маршрутам
func getAccessibleIslands(startIsland int, routes []Route) map[int]bool {
	graph := make(map[int][]int)
	for _, r := range routes {
		graph[r.From] = append(graph[r.From], r.To)
	}

	visited := make(map[int]bool)

	var dfs func(island int)
	dfs = func(island int) {
		if visited[island] {
			return
		}
		visited[island] = true
		for _, neig := range graph[island] {
			dfs(neig)
		}
	}

	dfs(startIsland)
	return visited
}

type StampNode struct {
	*Stamp
	Children []int
}

func (sn *StampNode) ParentID() int {
	if len(sn.Requires) > 0 {
		return sn.Requires[0] // по условию у штампа не более одной зависимости
	}
	return -1
}

func (sn *StampNode) String() string {
	return fmt.Sprintf("{Stamp:%+v Children:%v}", *sn.Stamp, sn.Children)
}

func NewStampNode(stamp *Stamp) *StampNode {
	return &StampNode{Stamp: stamp}
}

// buildStampsTree строит дерево зависимостей доступных штампов (по Requires) с фиктиным корнем -1.
// Штамп доступен, если остров есть в списке, возможно получить все его зависимости или у него нет зависимостей.
func buildStampsTree(stamps []Stamp, islands map[int]bool) map[int]*StampNode {
	nodes := make([]StampNode, 0, len(stamps)+1)

	// фиктивный корень
	root := &Stamp{StampID: -1}
	nodes = append(nodes, StampNode{Stamp: root})

	// создаем ноды только для штампов с доступных островов
	for i := range stamps {
		stamp := &stamps[i]
		if islands[stamp.IslandID] {
			nodes = append(nodes, StampNode{Stamp: stamp})
		}
	}

	// строим дерево зависимостей

	tree := make(map[int]*StampNode, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		tree[node.StampID] = node
	}

	for i := 1; i < len(nodes); i++ { // за исключением корня
		node := &nodes[i]
		parent := tree[node.ParentID()]
		if parent != nil {
			parent.Children = append(parent.Children, node.StampID)
		}
	}

	// отбираем только достижимые штампы

	visited := make(map[int]*StampNode)

	var dfs func(id int)
	dfs = func(id int) {
		node := tree[id]
		if node == nil {
			panic(fmt.Errorf("nil node detected %d", id))
		}
		visited[id] = node
		for _, child := range node.Children {
			dfs(child)
		}
	}

	dfs(-1)
	return visited
}

type requestWriter struct {
	conn   *net.TCPConn
	host   string
	tourID string
	buf    bytes.Buffer
}

func (w *requestWriter) writeRequest(urlPref string) {
	w.buf.WriteString("GET ")
	w.buf.WriteString(urlPref)
	w.buf.WriteString(w.tourID)
	w.buf.WriteString(" HTTP/1.1\r\nHost: ")
	w.buf.WriteString(w.host)
	w.buf.WriteString("\r\n\r\n")
}

func (w *requestWriter) flush() error {
	_, err := w.buf.WriteTo(w.conn)
	return err
}

type responseReader struct {
	br  *bufio.Reader
	buf []byte
}

func (r *responseReader) readResponse(resp EasyJSONUnmarshaler) error {
	if err := r.fastReadResponse(); err != nil {
		return err
	}
	l := jlexer_Lexer{Data: r.buf}
	resp.UnmarshalEasyJSON(&l)
	return l.Error()
}

func unsafeString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (r *responseReader) fastReadResponse() error {
	_, err := r.br.ReadSlice('\n') // skip status line
	if err != nil {
		return err
	}

	var contentLength int = -1
	var chunked bool

	for {
		line, err := r.br.ReadSlice('\n')
		if err != nil {
			return err
		}
		if len(line) == 2 && line[0] == '\r' && line[1] == '\n' {
			break
		}

		// Разбираем заголовок
		header := unsafeString(line)
		colon := strings.IndexByte(header, ':')
		if colon == -1 {
			continue
		}
		key := header[:colon]
		value := strings.TrimSpace(header[colon+1:])

		switch {
		case strings.EqualFold(key, "Transfer-Encoding"):
			chunked = strings.Contains(value, "chunked")
		case strings.EqualFold(key, "Content-Length"):
			contentLength, _ = strconv.Atoi(value)
		}
	}

	if chunked {
		return r.readChunkedBody()
	} else if contentLength >= 0 {
		// Читаем ровно contentLength байт
		r.buf = append(r.buf[:0], make([]byte, contentLength)...)
		_, err := io.ReadFull(r.br, r.buf)
		return err
	} else {
		// Нет ни Content-Length, ни chunked – читаем до EOF
		buf := bytes.NewBuffer(r.buf[:0])
		_, err := buf.ReadFrom(r.br)
		r.buf = buf.Bytes()
		return err
	}
}

// readChunkedBody читает тело в формате chunked.
func (r *responseReader) readChunkedBody() error {
	r.buf = r.buf[:0]
	for {
		// Читаем размер чанка в hex
		line, err := r.br.ReadSlice('\n')
		if err != nil {
			return err
		}
		hexStr := strings.TrimSpace(unsafeString(line))
		chunkSize, err := strconv.ParseInt(hexStr, 16, 64)
		if err != nil {
			return err
		}

		if chunkSize == 0 { // Конец чанков
			r.br.Discard(2) // пропускаем завершающие \r\n
			return nil
		}

		// Читаем данные чанка
		n := len(r.buf)
		r.buf = append(r.buf, make([]byte, chunkSize)...)
		if _, err = io.ReadFull(r.br, r.buf[n:]); err != nil {
			return err
		}
		r.br.Discard(2) // Пропускаем \r\n после чанка
	}
}

func GetParams(baseURL, tourID string) (params Params, err error) {
	var zero Params

	uri, _ := url.Parse(baseURL) // корректный URL вида http://127.0.0.1:<port>
	addrPort, _ := netip.ParseAddrPort(uri.Host)
	tcpAddr := net.TCPAddrFromAddrPort(addrPort)

	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return zero, err
	}
	defer conn.Close()

	w := requestWriter{conn: conn, host: uri.Host, tourID: tourID}
	r := responseReader{br: bufio.NewReaderSize(conn, 1<<19), buf: make([]byte, 1<<19)}

	var tour Tour
	var routesResp RoutesResponse
	var stampsResp StampsResponse

	for _, urlPref := range []string{"/tour/", "/routes?tour_id=", "/stamps?tour_id="} {
		w.writeRequest(urlPref)
	}
	if err := w.flush(); err != nil {
		return zero, err
	}
	if err := conn.CloseWrite(); err != nil {
		return zero, err
	}

	if err := r.readResponse(&tour); err != nil {
		return zero, err
	}
	if err := r.readResponse(&routesResp); err != nil {
		return zero, err
	}
	if err := r.readResponse(&stampsResp); err != nil {
		return zero, err
	}

	return Params{
		Tour:   tour,
		Routes: routesResp.Routes,
		Stamps: stampsResp.Stamps,
	}, nil
}

func run(in io.Reader, out io.Writer, solve solveFunc) {
	br := NewReader(in)
	bw := NewWriter(out)
	defer bw.Flush()

	var baseURL string
	var tourID string
	ScanWord(br, &baseURL, &tourID)

	params, err := GetParams(baseURL, tourID)
	if err != nil {
		panic(err)
	}

	ans := solve(params)
	if len(ans) == 0 {
		bw.WriteString("0\n")
	} else {
		PrintIntLn(bw, len(ans))
		PrintIntsLn(bw, ans)
	}
}

func main() {
	run(os.Stdin, os.Stdout, solve)
}

// -- easyjson --

func (v *Tour) UnmarshalEasyJSON(l *jlexer_Lexer) {
	easyjson820cd4beDecodeCoderun202607Contest8V2Dto(l, v)
}

func (v *Route) UnmarshalEasyJSON(l *jlexer_Lexer) {
	easyjson44127895DecodeCoderun202607Contest8V2Dto1(l, v)
}

func (v *RoutesResponse) UnmarshalEasyJSON(l *jlexer_Lexer) {
	easyjson44127895DecodeCoderun202607Contest8V2Dto(l, v)
}

func (v *StampsResponse) UnmarshalEasyJSON(l *jlexer_Lexer) {
	easyjson738f9efDecodeCoderun202607Contest8V2Dto(l, v)
}

func (v *Stamp) UnmarshalEasyJSON(l *jlexer_Lexer) {
	easyjson738f9efDecodeCoderun202607Contest8V2Dto1(l, v)
}

func easyjson820cd4beDecodeCoderun202607Contest8V2Dto(in *jlexer_Lexer, out *Tour) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		switch key {
		case "tour_id":
			if in.IsNull() {
				in.Skip()
			} else {
				out.TourID = string(in.String())
			}
		case "start_island":
			if in.IsNull() {
				in.Skip()
			} else {
				out.StartIsland = int(in.Int())
			}
		case "max_stamps":
			if in.IsNull() {
				in.Skip()
			} else {
				out.MaxStamps = int(in.Int())
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

func easyjson44127895DecodeCoderun202607Contest8V2Dto1(in *jlexer_Lexer, out *Route) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		switch key {
		case "from":
			if in.IsNull() {
				in.Skip()
			} else {
				out.From = int(in.Int())
			}
		case "to":
			if in.IsNull() {
				in.Skip()
			} else {
				out.To = int(in.Int())
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

func easyjson44127895DecodeCoderun202607Contest8V2Dto(in *jlexer_Lexer, out *RoutesResponse) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		switch key {
		case "routes":
			if in.IsNull() {
				in.Skip()
				out.Routes = nil
			} else {
				in.Delim('[')
				if out.Routes == nil {
					if !in.IsDelim(']') {
						out.Routes = make([]Route, 0, 4)
					} else {
						out.Routes = []Route{}
					}
				} else {
					out.Routes = (out.Routes)[:0]
				}
				for !in.IsDelim(']') {
					var v1 Route
					if in.IsNull() {
						in.Skip()
					} else {
						(v1).UnmarshalEasyJSON(in)
					}
					out.Routes = append(out.Routes, v1)
					in.WantComma()
				}
				in.Delim(']')
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

func easyjson738f9efDecodeCoderun202607Contest8V2Dto1(in *jlexer_Lexer, out *Stamp) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		switch key {
		case "stamp_id":
			if in.IsNull() {
				in.Skip()
			} else {
				out.StampID = int(in.Int())
			}
		case "island_id":
			if in.IsNull() {
				in.Skip()
			} else {
				out.IslandID = int(in.Int())
			}
		case "rarity":
			if in.IsNull() {
				in.Skip()
			} else {
				out.Rarity = int(in.Int())
			}
		case "requires":
			if in.IsNull() {
				in.Skip()
				out.Requires = nil
			} else {
				in.Delim('[')
				if out.Requires == nil {
					if !in.IsDelim(']') {
						out.Requires = make([]int, 0, 8)
					} else {
						out.Requires = []int{}
					}
				} else {
					out.Requires = (out.Requires)[:0]
				}
				for !in.IsDelim(']') {
					var v4 int
					if in.IsNull() {
						in.Skip()
					} else {
						v4 = int(in.Int())
					}
					out.Requires = append(out.Requires, v4)
					in.WantComma()
				}
				in.Delim(']')
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

func easyjson738f9efDecodeCoderun202607Contest8V2Dto(in *jlexer_Lexer, out *StampsResponse) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeFieldName(false)
		in.WantColon()
		switch key {
		case "stamps":
			if in.IsNull() {
				in.Skip()
				out.Stamps = nil
			} else {
				in.Delim('[')
				if out.Stamps == nil {
					if !in.IsDelim(']') {
						out.Stamps = make([]Stamp, 0, 1)
					} else {
						out.Stamps = []Stamp{}
					}
				} else {
					out.Stamps = (out.Stamps)[:0]
				}
				for !in.IsDelim(']') {
					var v1 Stamp
					if in.IsNull() {
						in.Skip()
					} else {
						(v1).UnmarshalEasyJSON(in)
					}
					out.Stamps = append(out.Stamps, v1)
					in.WantComma()
				}
				in.Delim(']')
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}

// == jlexer/lexer.go ==

// tokenKind determines type of a token.
type _jlexer_tokenKind byte

const (
	_jlexer_tokenUndef  _jlexer_tokenKind = iota // No token.
	_jlexer_tokenDelim                           // Delimiter: one of '{', '}', '[' or ']'.
	_jlexer_tokenString                          // A string literal, e.g. "abc\u1234"
	_jlexer_tokenNumber                          // Number literal, e.g. 1.5e5
	_jlexer_tokenBool                            // Boolean literal: true or false.
	_jlexer_tokenNull                            // null keyword.
)

// token describes a single _jlexer_token: type, position in the input and value.
type _jlexer_token struct {
	kind _jlexer_tokenKind // Type of a token.

	boolValue       bool   // Value if a boolean literal token.
	byteValueCloned bool   // true if byteValue was allocated and does not refer to original json body
	byteValue       []byte // Raw value of a token.
	delimValue      byte
}

// Lexer is a JSON lexer: it iterates over JSON tokens in a byte slice.
type jlexer_Lexer struct {
	Data []byte // Input data given to the lexer.

	start int           // Start of the current token.
	pos   int           // Current unscanned position in the input stream.
	token _jlexer_token // Last scanned token, if token.kind != tokenUndef.

	firstElement bool // Whether current element is the first in array or an object.
	wantSep      byte // A comma or a colon character, which need to occur before a token.

	UseMultipleErrors bool                 // If we want to use multiple errors.
	fatalError        error                // Fatal error occurred during lexing. It is usually a syntax error.
	multipleErrors    []*jlexer_LexerError // Semantic errors occurred during lexing. Marshalling will be continued after finding this errors.
}

// FetchToken scans the input for the next token.
func (r *jlexer_Lexer) FetchToken() {
	r.token.kind = _jlexer_tokenUndef
	r.start = r.pos

	// Check if r.Data has r.pos element
	// If it doesn't, it mean corrupted input data
	if len(r.Data) < r.pos {
		r.errParse("Unexpected end of data")
		return
	}
	// Determine the type of a token by skipping whitespace and reading the
	// first character.
	for _, c := range r.Data[r.pos:] {
		switch c {
		case ':', ',':
			if r.wantSep == c {
				r.pos++
				r.start++
				r.wantSep = 0
			} else {
				r.errSyntax()
			}

		case ' ', '\t', '\r', '\n':
			r.pos++
			r.start++

		case '"':
			if r.wantSep != 0 {
				r.errSyntax()
			}

			r.token.kind = _jlexer_tokenString
			r.fetchString()
			return

		case '{', '[':
			if r.wantSep != 0 {
				r.errSyntax()
			}
			r.firstElement = true
			r.token.kind = _jlexer_tokenDelim
			r.token.delimValue = r.Data[r.pos]
			r.pos++
			return

		case '}', ']':
			if !r.firstElement && (r.wantSep != ',') {
				r.errSyntax()
			}
			r.wantSep = 0
			r.token.kind = _jlexer_tokenDelim
			r.token.delimValue = r.Data[r.pos]
			r.pos++
			return

		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-':
			if r.wantSep != 0 {
				r.errSyntax()
			}
			r.token.kind = _jlexer_tokenNumber
			r.fetchNumber()
			return

		case 'n':
			if r.wantSep != 0 {
				r.errSyntax()
			}

			r.token.kind = _jlexer_tokenNull
			r.fetchNull()
			return

		case 't':
			if r.wantSep != 0 {
				r.errSyntax()
			}

			r.token.kind = _jlexer_tokenBool
			r.token.boolValue = true
			r.fetchTrue()
			return

		case 'f':
			if r.wantSep != 0 {
				r.errSyntax()
			}

			r.token.kind = _jlexer_tokenBool
			r.token.boolValue = false
			r.fetchFalse()
			return

		default:
			r.errSyntax()
			return
		}
	}
	r.fatalError = io.EOF
	return
}

// isTokenEnd returns true if the char can follow a non-delimiter token
func jlexer_isTokenEnd(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '[' || c == ']' || c == '{' || c == '}' || c == ',' || c == ':'
}

// fetchNull fetches and checks remaining bytes of null keyword.
func (r *jlexer_Lexer) fetchNull() {
	r.pos += 4
	if r.pos > len(r.Data) ||
		r.Data[r.pos-3] != 'u' ||
		r.Data[r.pos-2] != 'l' ||
		r.Data[r.pos-1] != 'l' ||
		(r.pos != len(r.Data) && !jlexer_isTokenEnd(r.Data[r.pos])) {

		r.pos -= 4
		r.errSyntax()
	}
}

// fetchTrue fetches and checks remaining bytes of true keyword.
func (r *jlexer_Lexer) fetchTrue() {
	r.pos += 4
	if r.pos > len(r.Data) ||
		r.Data[r.pos-3] != 'r' ||
		r.Data[r.pos-2] != 'u' ||
		r.Data[r.pos-1] != 'e' ||
		(r.pos != len(r.Data) && !jlexer_isTokenEnd(r.Data[r.pos])) {

		r.pos -= 4
		r.errSyntax()
	}
}

// fetchFalse fetches and checks remaining bytes of false keyword.
func (r *jlexer_Lexer) fetchFalse() {
	r.pos += 5
	if r.pos > len(r.Data) ||
		r.Data[r.pos-4] != 'a' ||
		r.Data[r.pos-3] != 'l' ||
		r.Data[r.pos-2] != 's' ||
		r.Data[r.pos-1] != 'e' ||
		(r.pos != len(r.Data) && !jlexer_isTokenEnd(r.Data[r.pos])) {

		r.pos -= 5
		r.errSyntax()
	}
}

// fetchNumber scans a number literal token.
func (r *jlexer_Lexer) fetchNumber() {
	hasE := false
	afterE := false
	hasDot := false

	r.pos++
	for i, c := range r.Data[r.pos:] {
		switch {
		case c >= '0' && c <= '9':
			afterE = false
		case c == '.' && !hasDot:
			hasDot = true
		case (c == 'e' || c == 'E') && !hasE:
			hasE = true
			hasDot = true
			afterE = true
		case (c == '+' || c == '-') && afterE:
			afterE = false
		default:
			r.pos += i
			if !jlexer_isTokenEnd(c) {
				r.errSyntax()
			} else {
				r.token.byteValue = r.Data[r.start:r.pos]
			}
			return
		}
	}

	r.pos = len(r.Data)
	r.token.byteValue = r.Data[r.start:]
}

// findStringLen tries to scan into the string literal for ending quote char to determine required size.
// The size will be exact if no escapes are present and may be inexact if there are escaped chars.
func jlexer_findStringLen(data []byte) (isValid bool, length int) {
	for {
		idx := bytes.IndexByte(data, '"')
		if idx == -1 {
			return false, len(data)
		}
		if idx == 0 || (idx > 0 && data[idx-1] != '\\') {
			return true, length + idx
		}

		// count \\\\\\\ sequences. even number of slashes means quote is not really escaped
		cnt := 1
		for idx-cnt-1 >= 0 && data[idx-cnt-1] == '\\' {
			cnt++
		}
		if cnt%2 == 0 {
			return true, length + idx
		}

		length += idx + 1
		data = data[idx+1:]
	}
}

// unescapeStringToken performs unescaping of string token.
// if no escaping is needed, original string is returned, otherwise - a new one allocated
func (r *jlexer_Lexer) unescapeStringToken() (err error) {
	data := r.token.byteValue
	var unescapedData []byte

	for {
		i := bytes.IndexByte(data, '\\')
		if i == -1 {
			break
		}

		escapedRune, escapedBytes, err := _jlexer_decodeEscape(data[i:])
		if err != nil {
			r.errParse(err.Error())
			return err
		}

		if unescapedData == nil {
			unescapedData = make([]byte, 0, len(r.token.byteValue))
		}

		var d [4]byte
		s := utf8.EncodeRune(d[:], escapedRune)
		unescapedData = append(unescapedData, data[:i]...)
		unescapedData = append(unescapedData, d[:s]...)

		data = data[i+escapedBytes:]
	}

	if unescapedData != nil {
		r.token.byteValue = append(unescapedData, data...)
		r.token.byteValueCloned = true
	}
	return
}

// getu4 decodes \uXXXX from the beginning of s, returning the hex value,
// or it returns -1.
func _jlexer_getu4(s []byte) rune {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return -1
	}
	var val rune
	for i := 2; i < len(s) && i < 6; i++ {
		var v byte
		c := s[i]
		switch c {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			v = c - '0'
		case 'a', 'b', 'c', 'd', 'e', 'f':
			v = c - 'a' + 10
		case 'A', 'B', 'C', 'D', 'E', 'F':
			v = c - 'A' + 10
		default:
			return -1
		}

		val <<= 4
		val |= rune(v)
	}
	return val
}

// decodeEscape processes a single escape sequence and returns number of bytes processed.
func _jlexer_decodeEscape(data []byte) (decoded rune, bytesProcessed int, err error) {
	if len(data) < 2 {
		return 0, 0, errors.New("incorrect escape symbol \\ at the end of token")
	}

	c := data[1]
	switch c {
	case '"', '/', '\\':
		return rune(c), 2, nil
	case 'b':
		return '\b', 2, nil
	case 'f':
		return '\f', 2, nil
	case 'n':
		return '\n', 2, nil
	case 'r':
		return '\r', 2, nil
	case 't':
		return '\t', 2, nil
	case 'u':
		rr := _jlexer_getu4(data)
		if rr < 0 {
			return 0, 0, errors.New("incorrectly escaped \\uXXXX sequence")
		}

		read := 6
		if utf16.IsSurrogate(rr) {
			rr1 := _jlexer_getu4(data[read:])
			if dec := utf16.DecodeRune(rr, rr1); dec != unicode.ReplacementChar {
				read += 6
				rr = dec
			} else {
				rr = unicode.ReplacementChar
			}
		}
		return rr, read, nil
	}

	return 0, 0, errors.New("incorrectly escaped bytes")
}

// fetchString scans a string literal token.
func (r *jlexer_Lexer) fetchString() {
	r.pos++
	data := r.Data[r.pos:]

	isValid, length := jlexer_findStringLen(data)
	if !isValid {
		r.pos += length
		r.errParse("unterminated string literal")
		return
	}
	r.token.byteValue = data[:length]
	r.pos += length + 1 // skip closing '"' as well
}

// scanToken scans the next token if no token is currently available in the lexer.
func (r *jlexer_Lexer) scanToken() {
	if r.token.kind != _jlexer_tokenUndef || r.fatalError != nil {
		return
	}

	r.FetchToken()
}

// consume resets the current token to allow scanning the next one.
func (r *jlexer_Lexer) consume() {
	r.token.kind = _jlexer_tokenUndef
	r.token.byteValueCloned = false
	r.token.delimValue = 0
}

// Ok returns true if no error (including io.EOF) was encountered during scanning.
func (r *jlexer_Lexer) Ok() bool {
	return r.fatalError == nil
}

const _jlexer_maxErrorContextLen = 13

func (r *jlexer_Lexer) errParse(what string) {
	if r.fatalError == nil {
		var str string
		if len(r.Data)-r.pos <= _jlexer_maxErrorContextLen {
			str = string(r.Data)
		} else {
			str = string(r.Data[r.pos:r.pos+_jlexer_maxErrorContextLen-3]) + "..."
		}
		r.fatalError = &jlexer_LexerError{
			Reason: what,
			Offset: r.pos,
			Data:   str,
		}
	}
}

func (r *jlexer_Lexer) errSyntax() {
	r.errParse("syntax error")
}

func (r *jlexer_Lexer) errInvalidToken(expected string) {
	if r.fatalError != nil {
		return
	}
	if r.UseMultipleErrors {
		r.pos = r.start
		r.consume()
		r.SkipRecursive()
		switch expected {
		case "[":
			r.token.delimValue = ']'
			r.token.kind = _jlexer_tokenDelim
		case "{":
			r.token.delimValue = '}'
			r.token.kind = _jlexer_tokenDelim
		}
		r.addNonfatalError(&jlexer_LexerError{
			Reason: fmt.Sprintf("expected %s", expected),
			Offset: r.start,
			Data:   string(r.Data[r.start:r.pos]),
		})
		return
	}

	var str string
	if len(r.token.byteValue) <= _jlexer_maxErrorContextLen {
		str = string(r.token.byteValue)
	} else {
		str = string(r.token.byteValue[:_jlexer_maxErrorContextLen-3]) + "..."
	}
	r.fatalError = &jlexer_LexerError{
		Reason: fmt.Sprintf("expected %s", expected),
		Offset: r.pos,
		Data:   str,
	}
}

func (r *jlexer_Lexer) GetPos() int {
	return r.pos
}

// Delim consumes a token and verifies that it is the given delimiter.
func (r *jlexer_Lexer) Delim(c byte) {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}

	if !r.Ok() || r.token.delimValue != c {
		r.consume() // errInvalidToken can change token if UseMultipleErrors is enabled.
		r.errInvalidToken(string([]byte{c}))
	} else {
		r.consume()
	}
}

// IsDelim returns true if there was no scanning error and next token is the given delimiter.
func (r *jlexer_Lexer) IsDelim(c byte) bool {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	return !r.Ok() || r.token.delimValue == c
}

// Null verifies that the next token is null and consumes it.
func (r *jlexer_Lexer) Null() {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenNull {
		r.errInvalidToken("null")
	}
	r.consume()
}

// IsNull returns true if the next token is a null keyword.
func (r *jlexer_Lexer) IsNull() bool {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	return r.Ok() && r.token.kind == _jlexer_tokenNull
}

// Skip skips a single token.
func (r *jlexer_Lexer) Skip() {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	r.consume()
}

// SkipRecursive skips next array or object completely, or just skips a single token if not
// an array/object.
//
// Note: no syntax validation is performed on the skipped data.
func (r *jlexer_Lexer) SkipRecursive() {
	r.scanToken()
	var start, end byte
	startPos := r.start

	switch r.token.delimValue {
	case '{':
		start, end = '{', '}'
	case '[':
		start, end = '[', ']'
	default:
		r.consume()
		return
	}

	r.consume()

	level := 1
	inQuotes := false
	wasEscape := false

	for i, c := range r.Data[r.pos:] {
		switch {
		case c == start && !inQuotes:
			level++
		case c == end && !inQuotes:
			level--
			if level == 0 {
				r.pos += i + 1
				if !json.Valid(r.Data[startPos:r.pos]) {
					r.pos = len(r.Data)
					r.fatalError = &jlexer_LexerError{
						Reason: "skipped array/object json value is invalid",
						Offset: r.pos,
						Data:   string(r.Data[r.pos:]),
					}
				}
				return
			}
		case c == '\\' && inQuotes:
			wasEscape = !wasEscape
			continue
		case c == '"' && inQuotes:
			inQuotes = wasEscape
		case c == '"':
			inQuotes = true
		}
		wasEscape = false
	}
	r.pos = len(r.Data)
	r.fatalError = &jlexer_LexerError{
		Reason: "EOF reached while skipping array/object or token",
		Offset: r.pos,
		Data:   string(r.Data[r.pos:]),
	}
}

// Raw fetches the next item recursively as a data slice
func (r *jlexer_Lexer) Raw() []byte {
	r.SkipRecursive()
	if !r.Ok() {
		return nil
	}
	return r.Data[r.start:r.pos]
}

// IsStart returns whether the lexer is positioned at the start
// of an input string.
func (r *jlexer_Lexer) IsStart() bool {
	return r.pos == 0
}

// Consumed reads all remaining bytes from the input, publishing an error if
// there is anything but whitespace remaining.
func (r *jlexer_Lexer) Consumed() {
	if r.pos > len(r.Data) || !r.Ok() {
		return
	}

	for _, c := range r.Data[r.pos:] {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			r.AddError(&jlexer_LexerError{
				Reason: "invalid character '" + string(c) + "' after top-level value",
				Offset: r.pos,
				Data:   string(r.Data[r.pos:]),
			})
			return
		}

		r.pos++
		r.start++
	}
}

func (r *jlexer_Lexer) unsafeString(skipUnescape bool) (string, []byte) {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenString {
		r.errInvalidToken("string")
		return "", nil
	}
	if !skipUnescape {
		if err := r.unescapeStringToken(); err != nil {
			r.errInvalidToken("string")
			return "", nil
		}
	}

	bytes := r.token.byteValue
	ret := _jlexer_bytesToStr(r.token.byteValue)
	r.consume()
	return ret, bytes
}

// UnsafeString returns the string value if the token is a string literal.
//
// Warning: returned string may point to the input buffer, so the string should not outlive
// the input buffer. Intended pattern of usage is as an argument to a switch statement.
func (r *jlexer_Lexer) UnsafeString() string {
	ret, _ := r.unsafeString(false)
	return ret
}

// UnsafeBytes returns the byte slice if the token is a string literal.
func (r *jlexer_Lexer) UnsafeBytes() []byte {
	_, ret := r.unsafeString(false)
	return ret
}

// UnsafeFieldName returns current member name string token
func (r *jlexer_Lexer) UnsafeFieldName(skipUnescape bool) string {
	ret, _ := r.unsafeString(skipUnescape)
	return ret
}

// String reads a string literal.
func (r *jlexer_Lexer) String() string {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenString {
		r.errInvalidToken("string")
		return ""
	}
	if err := r.unescapeStringToken(); err != nil {
		r.errInvalidToken("string")
		return ""
	}
	var ret string
	if r.token.byteValueCloned {
		ret = _jlexer_bytesToStr(r.token.byteValue)
	} else {
		ret = string(r.token.byteValue)
	}
	r.consume()
	return ret
}

var (
	_intern_pool sync.Pool = sync.Pool{
		New: func() interface{} {
			return make(map[string]string)
		},
	}
)

// Bytes returns b converted to a string, interned.
func _intern_Bytes(b []byte) string {
	m := _intern_pool.Get().(map[string]string)
	c, ok := m[string(b)]
	if ok {
		_intern_pool.Put(m)
		return c
	}
	s := string(b)
	m[s] = s
	_intern_pool.Put(m)
	return s
}

// StringIntern reads a string literal, and performs string interning on it.
func (r *jlexer_Lexer) StringIntern() string {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenString {
		r.errInvalidToken("string")
		return ""
	}
	if err := r.unescapeStringToken(); err != nil {
		r.errInvalidToken("string")
		return ""
	}
	ret := _intern_Bytes(r.token.byteValue)
	r.consume()
	return ret
}

// Bytes reads a string literal and base64 decodes it into a byte slice.
func (r *jlexer_Lexer) Bytes() []byte {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenString {
		r.errInvalidToken("string")
		return nil
	}
	if err := r.unescapeStringToken(); err != nil {
		r.errInvalidToken("string")
		return nil
	}
	ret := make([]byte, base64.StdEncoding.DecodedLen(len(r.token.byteValue)))
	n, err := base64.StdEncoding.Decode(ret, r.token.byteValue)
	if err != nil {
		r.fatalError = &jlexer_LexerError{
			Reason: err.Error(),
		}
		return nil
	}

	r.consume()
	return ret[:n]
}

// Bool reads a true or false boolean keyword.
func (r *jlexer_Lexer) Bool() bool {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenBool {
		r.errInvalidToken("bool")
		return false
	}
	ret := r.token.boolValue
	r.consume()
	return ret
}

func (r *jlexer_Lexer) number() string {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() || r.token.kind != _jlexer_tokenNumber {
		r.errInvalidToken("number")
		return ""
	}
	ret := _jlexer_bytesToStr(r.token.byteValue)
	r.consume()
	return ret
}

func (r *jlexer_Lexer) Uint8() uint8 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return uint8(n)
}

func (r *jlexer_Lexer) Uint16() uint16 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return uint16(n)
}

func (r *jlexer_Lexer) Uint32() uint32 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return uint32(n)
}

func (r *jlexer_Lexer) Uint64() uint64 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return n
}

func (r *jlexer_Lexer) Uint() uint {
	return uint(r.Uint64())
}

func (r *jlexer_Lexer) Int8() int8 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 8)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return int8(n)
}

func (r *jlexer_Lexer) Int16() int16 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 16)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return int16(n)
}

func (r *jlexer_Lexer) Int32() int32 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return int32(n)
}

func (r *jlexer_Lexer) Int64() int64 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return n
}

func (r *jlexer_Lexer) Int() int {
	return int(r.Int64())
}

func (r *jlexer_Lexer) Uint8Str() uint8 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return uint8(n)
}

func (r *jlexer_Lexer) Uint16Str() uint16 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return uint16(n)
}

func (r *jlexer_Lexer) Uint32Str() uint32 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return uint32(n)
}

func (r *jlexer_Lexer) Uint64Str() uint64 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return n
}

func (r *jlexer_Lexer) UintStr() uint {
	return uint(r.Uint64Str())
}

func (r *jlexer_Lexer) UintptrStr() uintptr {
	return uintptr(r.Uint64Str())
}

func (r *jlexer_Lexer) Int8Str() int8 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 8)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return int8(n)
}

func (r *jlexer_Lexer) Int16Str() int16 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 16)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return int16(n)
}

func (r *jlexer_Lexer) Int32Str() int32 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return int32(n)
}

func (r *jlexer_Lexer) Int64Str() int64 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return n
}

func (r *jlexer_Lexer) IntStr() int {
	return int(r.Int64Str())
}

func (r *jlexer_Lexer) Float32() float32 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseFloat(s, 32)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return float32(n)
}

func (r *jlexer_Lexer) Float32Str() float32 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}
	n, err := strconv.ParseFloat(s, 32)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return float32(n)
}

func (r *jlexer_Lexer) Float64() float64 {
	s := r.number()
	if !r.Ok() {
		return 0
	}

	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   s,
		})
	}
	return n
}

func (r *jlexer_Lexer) Float64Str() float64 {
	s, b := r.unsafeString(false)
	if !r.Ok() {
		return 0
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		r.addNonfatalError(&jlexer_LexerError{
			Offset: r.start,
			Reason: err.Error(),
			Data:   string(b),
		})
	}
	return n
}

func (r *jlexer_Lexer) Error() error {
	return r.fatalError
}

func (r *jlexer_Lexer) AddError(e error) {
	if r.fatalError == nil {
		r.fatalError = e
	}
}

func (r *jlexer_Lexer) AddNonFatalError(e error) {
	r.addNonfatalError(&jlexer_LexerError{
		Offset: r.start,
		Data:   string(r.Data[r.start:r.pos]),
		Reason: e.Error(),
	})
}

func (r *jlexer_Lexer) addNonfatalError(err *jlexer_LexerError) {
	if r.UseMultipleErrors {
		// We don't want to add errors with the same offset.
		if len(r.multipleErrors) != 0 && r.multipleErrors[len(r.multipleErrors)-1].Offset == err.Offset {
			return
		}
		r.multipleErrors = append(r.multipleErrors, err)
		return
	}
	r.fatalError = err
}

func (r *jlexer_Lexer) GetNonFatalErrors() []*jlexer_LexerError {
	return r.multipleErrors
}

// JsonNumber fetches and json.Number from 'encoding/json' package.
// Both int, float or string, contains them are valid values
func (r *jlexer_Lexer) JsonNumber() json.Number {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}
	if !r.Ok() {
		r.errInvalidToken("json.Number")
		return json.Number("")
	}

	switch r.token.kind {
	case _jlexer_tokenString:
		return json.Number(r.String())
	case _jlexer_tokenNumber:
		return json.Number(r.Raw())
	case _jlexer_tokenNull:
		r.Null()
		return json.Number("")
	default:
		r.errSyntax()
		return json.Number("")
	}
}

// Interface fetches an interface{} analogous to the 'encoding/json' package.
func (r *jlexer_Lexer) Interface() interface{} {
	if r.token.kind == _jlexer_tokenUndef && r.Ok() {
		r.FetchToken()
	}

	if !r.Ok() {
		return nil
	}
	switch r.token.kind {
	case _jlexer_tokenString:
		return r.String()
	case _jlexer_tokenNumber:
		return r.Float64()
	case _jlexer_tokenBool:
		return r.Bool()
	case _jlexer_tokenNull:
		r.Null()
		return nil
	}

	if r.token.delimValue == '{' {
		r.consume()

		ret := map[string]interface{}{}
		for !r.IsDelim('}') {
			key := r.String()
			r.WantColon()
			ret[key] = r.Interface()
			r.WantComma()
		}
		r.Delim('}')

		if r.Ok() {
			return ret
		} else {
			return nil
		}
	} else if r.token.delimValue == '[' {
		r.consume()

		ret := []interface{}{}
		for !r.IsDelim(']') {
			ret = append(ret, r.Interface())
			r.WantComma()
		}
		r.Delim(']')

		if r.Ok() {
			return ret
		} else {
			return nil
		}
	}
	r.errSyntax()
	return nil
}

// WantComma requires a comma to be present before fetching next token.
func (r *jlexer_Lexer) WantComma() {
	r.wantSep = ','
	r.firstElement = false
}

// WantColon requires a colon to be present before fetching next token.
func (r *jlexer_Lexer) WantColon() {
	r.wantSep = ':'
	r.firstElement = false
}

// == jlexer/bytestostr.go ==

// bytesToStr creates a string pointing at the slice to avoid copying.
//
// Warning: the string returned by the function should be used with care, as the whole input data
// chunk may be either blocked from being freed by GC because of a single string or the buffer.Data
// may be garbage-collected even when the string exists.
func _jlexer_bytesToStr(data []byte) string {
	// h := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	// shdr := reflect.StringHeader{Data: h.Data, Len: h.Len}
	// return *(*string)(unsafe.Pointer(&shdr))
	return *(*string)(unsafe.Pointer(&data))
}

// == jlexer/error.go ==

// LexerError implements the error interface and represents all possible errors that can be
// generated during parsing the JSON data.
type jlexer_LexerError struct {
	Reason string
	Offset int
	Data   string
}

func (l *jlexer_LexerError) Error() string {
	return fmt.Sprintf("parse error: %s near offset %d of '%s'", l.Reason, l.Offset, l.Data)
}

// -- inline:github.com/aaa2ppp/contestio --------------------------------------

type Int interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

func _appendInt[T Int](buf []byte, v T) []byte {
	signed := ^T(0) < 0
	if signed {
		return strconv.AppendInt(buf, int64(v), 10)
	} else {
		return strconv.AppendUint(buf, uint64(v), 10)
	}
}
func PrintIntLn[T Int](bw *Writer, a ...T) (int, error) {
	return _printSliceAppend(bw, _lineWO, _appendInt, a)
}
func PrintIntsLn[T Int](bw *Writer, a []T) (int, error) {
	return _printSliceAppend(bw, _lineWO, _appendInt, a)
}
func _must[T any](v T, err error) (T, error) {
	if err != nil && err != io.EOF {
		panic(err)
	}
	return v, err
}

type _writeOpts = WO
type WO struct {
	Begin string
	Sep   string
	End   string
}
type _appendValFunc[T any] func([]byte, T) []byte

func _printSliceAppendCommon[T any](bw *Writer, op _writeOpts, appendVal _appendValFunc[T], a []T) (int, error) {
	var buf []byte
	_, _ = bw.WriteString(op.Begin)
	for i := range a {
		if bw.Available() < len(bw.scratch) {
			buf = bw.scratch[:0]
		} else {
			buf = bw.AvailableBuffer()
		}
		if i > 0 {
			buf = append(buf, op.Sep...)
		}
		buf = appendVal(buf, a[i])
		if _, err := bw.Write(buf); err != nil {
			return i, err
		}
	}
	_, err := bw.WriteString(op.End)
	return len(a), err
}

var _lineWO = WO{Sep: " ", End: "\n"}

func _printSliceAppend[T any](bw *Writer, op _writeOpts, appenVal _appendValFunc[T], a []T) (int, error) {
	return _must(_printSliceAppendCommon(bw, op, appenVal, a))
}

const defaultBufSize = 4096

type br = bufio.Reader
type bw = bufio.Writer
type Reader struct{ br }

func NewReaderSize(r io.Reader, size int) *Reader {
	return &Reader{*bufio.NewReaderSize(r, size)}
}
func NewReader(r io.Reader) *Reader {
	return NewReaderSize(r, defaultBufSize)
}

type Writer struct {
	bw
	scratch [32]byte
}

func NewWriterSize(w io.Writer, size int) *Writer {
	return &Writer{bw: *bufio.NewWriterSize(w, size)}
}
func NewWriter(w io.Writer) *Writer {
	return NewWriterSize(w, defaultBufSize)
}

type _parseFunc[T any] func([]byte) (T, error)
type _parseToFunc[T any] func([]byte, T) error

func _parseToPtr[T any](token []byte, parse _parseFunc[T], p *T) error {
	v, err := parse(token)
	if err != nil {
		return err
	}
	*p = v
	return nil
}
func _scanVarsCommon[T any](br *Reader, stopAtEol bool, parseTo _parseToFunc[T], a []T) (int, error) {
	for i := range a {
		err := _skipSpace(br, stopAtEol)
		if err != nil {
			if err == io.EOF {
				return i, io.ErrUnexpectedEOF
			}
			return i, err
		}
		token, err := _nextToken(br)
		if err != nil && err != io.EOF {
			return i, err
		}
		if err := parseTo(token, a[i]); err != nil {
			return i, err
		}
	}
	return len(a), nil
}
func _scanVars[T any](br *Reader, parseTo _parseToFunc[T], a ...T) (int, error) {
	return _must(_scanVarsCommon(br, false, parseTo, a))
}

var ErrTokenTooLong = errors.New("token too long")

func _nextToken(br *Reader) ([]byte, error) {
	var buf []byte
	var err error
	i := 0
	fast := br.Buffered() > 0
	for i < br.Size() {
		if fast {
			buf, _ = br.Peek(br.Buffered())
		} else {
			buf, err = br.Peek(br.Buffered() + 1)
			if err != nil {
				_, _ = br.Discard(len(buf))
				return buf, err
			}
		}
		buf = buf[:br.Buffered()]
		for ; i < len(buf); i++ {
			if _isSpace(buf[i]) {
				_, _ = br.Discard(i)
				return buf[:i], nil
			}
		}
		fast = false
	}
	_, _ = br.Discard(len(buf))
	return buf, ErrTokenTooLong
}

var EOL = errors.New("EOL")
var _spaceTab = [256]bool{
	' ':  true,
	'\t': true,
	'\r': true,
	'\n': true,
}

func _isSpace(c byte) bool { return _spaceTab[c] }
func _skipSpace(br *Reader, stopAtEol bool) error {
	var buf []byte
	var err error
	fast := br.Buffered() > 0
	for {
		if fast {
			buf, _ = br.Peek(br.Buffered())
		} else {
			buf, err = br.Peek(br.Buffered() + 1)
			if err != nil {
				return err
			}
		}
		buf = buf[:br.Buffered()]
		for i, c := range buf {
			if stopAtEol && c == '\n' {
				_, _ = br.Discard(i + 1)
				return EOL
			}
			if !_isSpace(c) {
				_, _ = br.Discard(i)
				return nil
			}
		}
		_, _ = br.Discard(len(buf))
		fast = false
	}
}
func _parseWord[T ~string](token []byte) (T, error)       { return T(token), nil }
func _parseWordToPtr[T ~string](token []byte, p *T) error { return _parseToPtr(token, _parseWord, p) }
func ScanWord[T ~string](br *Reader, a ...*T) (int, error) {
	return _scanVars(br, _parseWordToPtr, a...)
}

// -- /inline:github.com/aaa2ppp/contestio -------------------------------------
