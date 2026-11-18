# `show --json`

`go-dicom show --json <file>` prints one JSON document that describes a DICOM
file: every data element in it, and the byte range each part of the file
occupies. Programs read it, so this page is the contract. The schema is
[show-json.schema.json](show-json.schema.json), a JSON Schema (draft 2020-12).

```
go-dicom show patient.dcm --json
```

`--json` describes the whole file; it cannot be combined with an element
selector (`file::0010,0010`).

## What `schemaVersion` 1 means

The document carries `"schemaVersion": 1`. The number is the version of the
shape of the document, not of go-dicom.

- Version 1 is the document described here. A reader written for version 1
  can read every document that says 1.
- **A property added to the document does not change the version.** A reader
  ignores properties it does not know. Everything below that is "present" stays
  present.
- **The version goes up when a property is renamed, removed or changes
  meaning**, and a reader should refuse a version it does not know rather than
  guess.
- What is not part of the contract: the order of properties inside an object,
  the white space (the document is indented by two spaces today), and the text
  of `warnings`, which is for a person.

Arrays keep file order: `fileMeta`, `dataSet`, `items`, `fragments` and
`regions` list their members as they lie in the file.

## The document

| Property | Type | Meaning |
|---|---|---|
| `schemaVersion` | integer | Always 1 here. |
| `file` | string | The path as given on the command line. |
| `size` | integer | The size of the file in bytes. |
| `preamble` | boolean | True for a Part 10 file (128-byte preamble, `DICM`, file meta group). False for a bare data set, which has none of those. |
| `regions` | array | The parts of the file in order, each `{name, offset, endOffset}`: `preamble` (0 to 128), `prefix` (128 to 132) and `fileMeta` for a Part 10 file, then `dataSet` to the end of the file. A bare data set has the one region `dataSet`. |
| `transferSyntax` | object | `uid` (the Transfer Syntax UID in the file meta group, empty if there is none), `explicitVR`, `littleEndian`, `deflated`. |
| `offsetsRelativeTo` | string | `file`, or `inflatedDataSet` for a deflated data set, whose elements are read after inflating it and whose offsets count from the start of the inflated bytes. |
| `fileMeta` | array | The elements of the file meta group, starting with Group Length (0002,0000) when the file has one. Empty for a bare data set. |
| `dataSet` | array | The elements of the data set. |
| `warnings` | array of strings | What the reader noticed and recovered from. Empty for a clean file. |

All of these are always present. All offsets are byte offsets, from the start
of the file unless `offsetsRelativeTo` says otherwise, and every `endOffset` is
the offset of the first byte after the thing it ends.

## An element

| Property | Type | Meaning |
|---|---|---|
| `tag` | string | `(gggg,eeee)`, upper-case hexadecimal. |
| `keyword`, `name` | string | From the dictionary; empty for a tag it does not know. |
| `vr` | string | The VR the element is read as. In an implicit VR data set it is the dictionary's; it is empty when nothing gives one. |
| `vm` | integer | How many values the element holds. 0 for an empty value, 1 for a sequence, for encapsulated pixel data and for a bulk value. |
| `value` | string, optional | The value as text, with a backslash between values. Numbers are written in decimal, an `AT` as a tag, text is decoded with the data set's character set. **Absent** for a sequence, for bulk binary (`OB`, `OW`, `OF`, `OD`, `OL`, `OV`, `UN`) and for a number VR whose length is not a whole number of values: those are given by their position alone. |
| `tagOffset` | integer | Where the 4 tag bytes start. |
| `vrOffset` | integer or null | Where the 2 VR bytes start. `null` when the element has no VR in the file (implicit VR). |
| `lengthOffset`, `lengthSize` | integer | Where the length field starts and how many bytes it takes (2 or 4). |
| `valueOffset` | integer | Where the value starts. |
| `valueLength` | integer | The bytes from `valueOffset` to the end of the content. A delimiter that closes an undefined-length value is not counted. |
| `undefinedLength` | boolean | True when the length field was `FFFFFFFF` and a delimiter ends the value. |
| `endOffset` | integer | The first byte after the element, its delimiter included. |
| `items` | array, optional | The items of a sequence, in order. Absent when it has none. |
| `fragments` | array, optional | The items of encapsulated Pixel Data: each `{tag, tagOffset, valueOffset, valueLength, endOffset}`; the first is the Basic Offset Table. |
| `delimiter` | object, optional | The Sequence Delimitation Item `(FFFE,E0DD)` that closes an undefined-length sequence or value: `{tag, tagOffset, endOffset}`. |

Bulk values are never read to produce the document: the position is all it
gives, so a 200 MB image costs the memory of its metadata.

An **item** is `{tag, tagOffset, lengthOffset, lengthSize, valueOffset,
valueLength, undefinedLength, endOffset, elements}` with `tag` always
`(FFFE,E000)`, `lengthSize` always 4, `elements` the item's data set, and
`delimiter` (the Item Delimitation Item `(FFFE,E00D)`) when its length is
undefined.

A **fragment** is `{tag, tagOffset, valueOffset, valueLength, endOffset}`.

## What a reader can rely on

- For each element, `tagOffset` to `endOffset` is exactly the bytes of the
  element in the file, and the elements of one level follow each other without
  a gap. This is what a hex viewer needs to colour the file.
- `valueOffset + valueLength` is where the content ends; with a `delimiter`,
  `delimiter.tagOffset` is the same offset.

## Checking a document

The schema is the check. With Python:

```
python -c "
import json, sys, jsonschema
schema = json.load(open('docs/show-json.schema.json'))
jsonschema.validate(json.load(sys.stdin), schema)" < document.json
```

The tests of the `cli` package check the output of every file of the pydicom
test corpus against the schema with a validator of their own, and fail on a
property the schema does not name, so a field cannot be added to the output
without being described here.
