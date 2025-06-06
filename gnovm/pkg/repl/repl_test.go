package repl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type step struct {
	Line   string
	Error  string
	Result string
}

var fixtures = []struct {
	Name      string
	CodeSteps []step
}{
	{
		Name: "Add new import",
		CodeSteps: []step{
			{
				Line:   "import \"fmt\"\nimport \"os\"",
				Result: "import \"fmt\"\nimport \"os\"\n",
			},
		},
	},
	{
		Name: "Add new constant",
		CodeSteps: []step{
			{
				Line:   "const test2, test3 = \"test_string2\", \"test_string3\"",
				Result: "const test2, test3 = \"test_string2\", \"test_string3\"\n",
			},
			{
				Line:   "const test = \"test_string\"",
				Result: "const test = \"test_string\"\n",
			},
		},
	},
	{
		Name: "Add struct and functions",
		CodeSteps: []step{
			{
				Line:   "type MyStruct struct { count int}",
				Result: "type MyStruct struct{ count int }\n",
			},
			{
				Line:   "func (s *MyStruct) Add(){s.count++}",
				Result: "func (s *MyStruct) Add()\t{ s.count++ }\n",
			},
		},
	},
	{
		Name: "Add new var",
		CodeSteps: []step{
			{
				Line:   "var test2, test3 string = \"test_string2\", \"test_string3\"",
				Result: "var test2, test3 string = \"test_string2\", \"test_string3\"\n",
			},
			{
				Line:   "var test int = 42",
				Result: "var test int = 42\n",
			},
		},
	},
	{
		Name: "Add wrong code",
		CodeSteps: []step{
			{
				Line:  "importasdasd",
				Error: "test/test1.gno:7:2-14: name importasdasd not declared",
			},
			{
				Line: "var a := 1",
				// we cannot check the entire error because it is different depending on the used Go version.
				Error: "error parsing code:",
			},
		},
	},
	{
		Name: "Add function and use it",
		CodeSteps: []step{
			{
				Line:   "func sum(a,b int)int{return a+b}",
				Result: "func sum(a, b int) int\t{ return a + b }\n",
			},
			{
				Line:   "import \"fmt\"",
				Result: "import \"fmt\"\n",
			},
			{
				Line:   "fmt.Println(sum(1,1))",
				Result: "2\n",
			},
		},
	},
	{
		Name: "All declarations at once",
		CodeSteps: []step{
			{
				Line:   "import \"fmt\"\nfunc sum(a,b int)int{return a+b}",
				Result: "import \"fmt\"\nfunc sum(a, b int) int\t{ return a + b }\n",
			},
			{
				Line:   "fmt.Println(sum(1,1))",
				Result: "2\n",
			},
		},
	},
	{
		Name: "Fibonacci",
		CodeSteps: []step{
			{
				Line: `
				func fib(n int)int {
					if n < 2 {
						return n
					}
					return fib(n-2) + fib(n-1)
				}
				`,
				Result: "func fib(n int) int {\n\tif n < 2 {\n\t\treturn n\n\t}\n\treturn fib(n-2) + fib(n-1)\n}\n",
			},
			{
				Line:   "println(fib(24))",
				Result: "46368\n",
			},
		},
	},
	{
		Name: "Meaning of life",
		CodeSteps: []step{
			{
				Line: `
				const (
					oneConst   = 1
					tenConst   = 10
					magicConst = 19
				)
				`,
				Result: "const (\n\toneConst\t= 1\n\ttenConst\t= 10\n\tmagicConst\t= 19\n)\n",
			},
			{
				Line:   "var outVar int",
				Result: "var outVar int\n",
			},
			{
				Line: `
				type MyStruct struct {
					counter int
				}
				
				func (s *MyStruct) Add() {
					s.counter++
				}
				
				func (s *MyStruct) Get() int {
					return s.counter
				}
				`,
				Result: "type MyStruct struct {\n\tcounter int\n}\nfunc (s *MyStruct) Add() {\n\ts.counter++\n}\nfunc (s *MyStruct) Get() int {\n\treturn s.counter\n}\n",
			},
			{
				Line: `
				ms := &MyStruct{counter: 10}

				ms.Add()
				ms.Add()

				outVar = ms.Get() + oneConst + tenConst + magicConst

				println(outVar)
				`,
				Result: "42\n",
			},
		},
	},
}

func TestRepl(t *testing.T) {
	for _, fix := range fixtures {
		fix := fix
		t.Run(fix.Name, func(t *testing.T) {
			r := NewRepl()
			for _, cs := range fix.CodeSteps {
				out, err := r.Process(cs.Line)
				if cs.Error == "" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.Contains(t, err.Error(), cs.Error)
				}

				require.Equal(t, cs.Result, out)
			}
		})
	}
}

func TestReplOpts(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	r := NewRepl(WithStd(nil, nil, nil), WithStore(nil))
	require.Nil(r.storeFunc())
	require.Nil(r.stderr)
	require.Nil(r.stdin)
	require.Nil(r.stdout)

	_, err := r.Process("import \"fmt\"")
	require.NoError(err)
}

func TestReplReset(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	r := NewRepl()

	_, err := r.Process("println(\"hi\")")
	require.NoError(err)
	o := r.Src()
	require.Contains(o, "generated by 'gno repl'")
	r.Reset()
	o = r.Src()
	require.NotContains(o, "generated by 'gno repl'")
}
