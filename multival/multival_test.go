package multival_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/multival"
)

func TestNewConstrainedList(t *testing.T) {
	constructor := func(v interface{}) interface{} { return v }
	cl := multival.New(constructor)

	if cl == nil {
		t.Fatal("multival.New() returned nil")
	}
	if cl.Length() != 0 {
		t.Errorf("Initial length should be 0, got %d", cl.Length())
	}
}

func TestAppend(t *testing.T) {
	cl := multival.NewStringList()

	err := cl.Append("hello")
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if cl.Length() != 1 {
		t.Errorf("After Append, length = %d, want 1", cl.Length())
	}

	val, err := cl.Get(0)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if val != "hello" {
		t.Errorf("Get(0) = %v, want 'hello'", val)
	}
}

func TestAppendMultiple(t *testing.T) {
	cl := multival.NewStringList()

	values := []string{"apple", "banana", "cherry"}
	for _, val := range values {
		if err := cl.Append(val); err != nil {
			t.Fatalf("Append(%s) error = %v", val, err)
		}
	}

	if cl.Length() != len(values) {
		t.Errorf("After Append x3, length = %d, want %d", cl.Length(), len(values))
	}

	for i, expected := range values {
		val, err := cl.Get(i)
		if err != nil {
			t.Fatalf("Get(%d) error = %v", i, err)
		}
		if val != expected {
			t.Errorf("Get(%d) = %v, want %v", i, val, expected)
		}
	}
}

func TestInsert(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")
	cl.Append("c")

	err := cl.Insert(1, "b")
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if cl.Length() != 3 {
		t.Errorf("After Insert, length = %d, want 3", cl.Length())
	}

	items := cl.Items()
	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

func TestRemove(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")
	cl.Append("b")
	cl.Append("c")

	err := cl.Remove(1)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if cl.Length() != 2 {
		t.Errorf("After Remove, length = %d, want 2", cl.Length())
	}

	items := cl.Items()
	expected := []string{"a", "c"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

func TestSet(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("old")

	err := cl.Set(0, "new")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	val, _ := cl.Get(0)
	if val != "new" {
		t.Errorf("Get(0) after Set = %v, want 'new'", val)
	}
}

func TestGetOutOfRange(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")

	_, err := cl.Get(5)
	if err == nil {
		t.Error("Get(5) should return error for out of range")
	}
}

func TestRemoveOutOfRange(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")

	err := cl.Remove(5)
	if err == nil {
		t.Error("Remove(5) should return error for out of range")
	}
}

func TestClear(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")
	cl.Append("b")
	cl.Append("c")

	cl.Clear()

	if cl.Length() != 0 {
		t.Errorf("After Clear, length = %d, want 0", cl.Length())
	}
}

func TestItems(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")
	cl.Append("b")
	cl.Append("c")

	items := cl.Items()

	if len(items) != 3 {
		t.Errorf("Items() length = %d, want 3", len(items))
	}

	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

func TestNewStringList(t *testing.T) {
	sl := multival.NewStringList()
	sl.Append("test")

	val, _ := sl.Get(0)
	if val != "test" {
		t.Errorf("StringList value = %v, want 'test'", val)
	}
}

func TestNewIntList(t *testing.T) {
	il := multival.NewIntList()
	il.Append(42)

	val, _ := il.Get(0)
	if val != 42 {
		t.Errorf("IntList value = %v, want 42", val)
	}
}

func TestNewFloatList(t *testing.T) {
	fl := multival.NewFloatList()
	fl.Append(3.14)

	val, _ := fl.Get(0)
	fval, ok := val.(float64)
	if !ok || fval != 3.14 {
		t.Errorf("FloatList value = %v, want 3.14", val)
	}
}

func TestNewFromValues(t *testing.T) {
	constructor := func(v interface{}) interface{} {
		switch val := v.(type) {
		case string:
			return val
		default:
			return fmt.Sprintf("%v", val)
		}
	}
	cl, err := multival.NewFromValues(constructor, "a", "b", "c")
	if err != nil {
		t.Fatalf("multival.NewFromValues() error = %v", err)
	}

	if cl.Length() != 3 {
		t.Errorf("NewFromValues length = %d, want 3", cl.Length())
	}
}

func TestConstructorTypeConversion(t *testing.T) {
	sl := multival.NewStringList()
	// StringList converts non-strings to strings
	sl.Append(123)

	val, _ := sl.Get(0)
	if val != "123" {
		t.Errorf("StringList converted 123 to %v, want '123'", val)
	}
}

func TestThreadSafety(t *testing.T) {
	cl := multival.NewStringList()

	// Test concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cl.Append("value")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = cl.Length()
			if cl.Length() > 0 {
				_, _ = cl.Get(0)
			}
		}
		done <- true
	}()

	<-done
	<-done
}

func TestExtend(t *testing.T) {
	cl := multival.NewStringList()
	cl.Append("a")

	values := []interface{}{"b", "c", "d"}
	err := cl.Extend(values)
	if err != nil {
		t.Fatalf("Extend() error = %v", err)
	}

	if cl.Length() != 4 {
		t.Errorf("After Extend, length = %d, want 4", cl.Length())
	}

	items := cl.Items()
	expected := []string{"a", "b", "c", "d"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

func TestSort(t *testing.T) {
	il := multival.NewIntList()
	il.Append(3)
	il.Append(1)
	il.Append(4)
	il.Append(1)
	il.Append(5)

	err := il.Sort(func(i, j interface{}) bool {
		return i.(int) < j.(int)
	})
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}

	items := il.Items()
	expected := []interface{}{1, 1, 3, 4, 5}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

func TestString(t *testing.T) {
	cl := multival.NewStringList()
	if cl.String() != "[]" {
		t.Errorf("Empty list String() = %s, want []", cl.String())
	}

	cl.Append("a")
	cl.Append("b")
	cl.Append("c")

	str := cl.String()
	if str != "[a, b, c]" {
		t.Errorf("String() = %s, want [a, b, c]", str)
	}
}

func TestEqual(t *testing.T) {
	cl1 := multival.New(func(v interface{}) interface{} { return v })
	cl1.Append("a")
	cl1.Append("b")

	cl2 := multival.New(func(v interface{}) interface{} { return v })
	cl2.Append("a")
	cl2.Append("b")

	if !cl1.Equal(cl2) {
		t.Error("Equal() should return true for identical lists")
	}

	cl2.Append("c")
	if cl1.Equal(cl2) {
		t.Error("Equal() should return false for different length lists")
	}

	cl3 := multival.New(func(v interface{}) interface{} { return v })
	cl3.Append("a")
	cl3.Append("x")

	if cl1.Equal(cl3) {
		t.Error("Equal() should return false for different items")
	}

	if cl1.Equal(nil) {
		t.Error("Equal() should return false when comparing with nil")
	}
}

func TestParseDICOMString(t *testing.T) {
	dicomString := "100.5\\200.3\\150.8"
	cl, err := multival.ParseDICOMString(dicomString, multival.DSConstructor)
	if err != nil {
		t.Fatalf("ParseDICOMString() error = %v", err)
	}

	if cl.Length() != 3 {
		t.Errorf("ParseDICOMString length = %d, want 3", cl.Length())
	}

	items := cl.Items()
	expected := []float64{100.5, 200.3, 150.8}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

func TestMultiValueToString(t *testing.T) {
	cl := multival.NewDSList()
	cl.Append(100.5)
	cl.Append(200.3)
	cl.Append(150.8)

	result := multival.MultiValueToString(cl, "\\")
	expected := "100.5\\200.3\\150.8"
	if result != expected {
		t.Errorf("MultiValueToString() = %s, want %s", result, expected)
	}
}

func TestVRConstructors(t *testing.T) {
	// Test DS Constructor
	ds := multival.NewDSList()
	ds.Append("100.5")
	ds.Append(200.3)

	if ds.Length() != 2 {
		t.Errorf("DS list length = %d, want 2", ds.Length())
	}

	// Test IS Constructor
	is := multival.NewISList()
	is.Append("42")
	is.Append(100)

	if is.Length() != 2 {
		t.Errorf("IS list length = %d, want 2", is.Length())
	}

	// Test CS Constructor
	cs := multival.NewCSList()
	cs.Append("image")
	val, _ := cs.Get(0)
	if val != "IMAGE" {
		t.Errorf("CS constructor should uppercase, got %v", val)
	}

	// Test LO Constructor
	lo := multival.NewLOList()
	longString := strings.Repeat("x", 100)
	lo.Append(longString)
	val, _ = lo.Get(0)
	if len(val.(string)) > 64 {
		t.Error("LO constructor should limit to 64 characters")
	}

	// Test SH Constructor
	sh := multival.NewSHList()
	longString = strings.Repeat("x", 100)
	sh.Append(longString)
	val, _ = sh.Get(0)
	if len(val.(string)) > 16 {
		t.Error("SH constructor should limit to 16 characters")
	}
}

func TestGetConstructorForVR(t *testing.T) {
	// Test that GetConstructorForVR returns appropriate constructors
	vrTests := []struct {
		vr       string
		testVal  interface{}
		expected interface{}
	}{
		{"DS", "123.45", 123.45},
		{"IS", "42", 42},
		{"CS", "value", "VALUE"},
		{"PN", "Doe^John", "Doe^John"},
	}

	for _, test := range vrTests {
		constructor := multival.GetConstructorForVR(test.vr)
		if constructor == nil {
			t.Errorf("GetConstructorForVR(%s) returned nil", test.vr)
			continue
		}

		result := constructor(test.testVal)
		if test.vr == "CS" {
			if result != test.expected {
				t.Errorf("VR %s: expected %v, got %v", test.vr, test.expected, result)
			}
		}
	}
}
