# FileBase

Low-level file I/O abstractions for DICOM with byte order (endianness) handling, Reader/Writer interfaces, position tracking, and buffer pooling.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/filebase"

// Reading
file, _ := os.Open("data.dcm")
reader := filebase.NewFileReader(file)
defer reader.Close()

b, _ := reader.ReadByte()
v16, _ := reader.ReadUint16()
v32, _ := reader.ReadUint32()

// Switch byte order
reader.SetByteOrder(filebase.BigEndian)

// Writing
writer := filebase.NewFileWriter(outputFile)
writer.WriteUint16(0x1234)
writer.WriteUint32(0x56789ABC)
writer.Flush()
writer.Close()

// Buffer pooling
pool := filebase.NewBufferPool(4096)
buf := pool.Get()
defer pool.Put(buf)
```

## API Reference

```go
type ByteOrder uint8 // LittleEndian, BigEndian

type Reader interface {
    Read(p []byte) (int, error)
    ReadByte() (byte, error)
    ReadBytes(p []byte) error
    ReadUint16() (uint16, error)
    ReadUint32() (uint32, error)
    Seek(offset int64, whence int) (int64, error)
    Tell() (int64, error)
    Close() error
    GetByteOrder() ByteOrder
    SetByteOrder(bo ByteOrder)
}

type Writer interface {
    Write(p []byte) (int, error)
    WriteByte(b byte) error
    WriteBytes(p []byte) error
    WriteUint16(v uint16) error
    WriteUint32(v uint32) error
    Seek(offset int64, whence int) (int64, error)
    Tell() (int64, error)
    Close() error
    Flush() error
    GetByteOrder() ByteOrder
    SetByteOrder(bo ByteOrder)
}

func NewFileReader(r io.ReadSeeker) *FileReader
func NewFileWriter(w io.ReadWriteSeeker) *FileWriter
func NewBufferPool(size int) *BufferPool

type Position struct { Offset int64; Tag uint32; VR string; Length uint32 }
```

## References

- [DICOM PS3.5 Section 5.1](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Data Element Structure and byte ordering
