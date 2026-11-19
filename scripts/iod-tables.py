#!/usr/bin/env python3
"""Generate validate/tables.go from DICOM PS3.3 and PS3.4.

What an instance of each SOP class must hold is in PS3.3: Annex A gives each
IOD its modules, with usage M, C or U, and Annex C each module its
attributes, with Type 1, 1C, 2, 2C or 3, nesting into sequences with ">" and
including macros by reference. PS3.4 Table B.5-1 names the IOD of each
Storage SOP class. This reads the standard's DocBook XML, as published at

    https://dicom.nema.org/medical/dicom/current/source/docbook/part03/part03.xml
    https://dicom.nema.org/medical/dicom/current/source/docbook/part04/part04.xml

and writes the tables as Go data. Nothing in them is typed by hand.

    python3 scripts/iod-tables.py part03.xml part04.xml | gofmt > validate/tables.go

Only what the tables state is taken. A condition is kept as text, and is also
written as data when PS3.3 states it in one of a few sentence forms that name
one attribute by its tag ("Required if Pixel Data Provider URL (0028,7FE0) is
not present", "... is present", "... has a Value of YES", "Image Type
(0008,0008) Value 1 is ORIGINAL or MIXED", "... is present and has a Value of
X"), followed by nothing but "May be present otherwise." when it is followed
at all. A condition that matches a form only in part, that another sentence
about presence qualifies, or whose attribute's name and tag disagree with the
tables, is prose, kept as text for the checker to report as not checked. The
numbers of each are printed on standard error. Enumerated Values and Defined
Terms are taken from a variablelist titled exactly so, in the attribute's
description or in the section its description names "for specialization"
when that section is about the attribute alone; a list that is ambiguous (two
for one value) is left out.

An enhanced multi-frame IOD's Functional Group Macros table is read too: each
macro with its table, its usage and condition, and where it may be ("May not
be used as a Shared Functional Group"), when the usage says.
"""
import re
import sys
import xml.etree.ElementTree as ET

D = "{http://docbook.org/ns/docbook}"
XID = "{http://www.w3.org/XML/1998/namespace}id"


def text(e):
    return " ".join("".join(e.itertext()).split()) if e is not None else ""


def load(path):
    root = ET.parse(path).getroot()
    ids = {e.get(XID): e for e in root.iter() if e.get(XID)}
    edition = re.search(r"(\d{4}[a-z])", text(root.find(D + "subtitle")))
    return root, ids, edition.group(1) if edition else ""


part3, ids3, edition3 = load(sys.argv[1])
part4, ids4, edition4 = load(sys.argv[2])

# Parent of every element, for finding the section a table sits in.
parent = {c: p for p in part3.iter() for c in p}


def section_of(e):
    while e is not None and e.tag != D + "section":
        e = parent.get(e)
    return e


def label_of(e):
    """A section's number, from its id: sect_C.7.1.1 is C.7.1.1."""
    i = e.get(XID) or ""
    return i.split("_", 1)[1] if "_" in i else i


def first_table(sec, caption_test):
    for t in sec.iter(D + "table"):
        if caption_test(text(t.find(D + "caption"))):
            return t
    return None


# ---------------------------------------------------------------- PS3.4 B.5-1

sop_classes = []  # (uid, name, iod section id)
for tr in ids4["table_B.5-1"].find(D + "tbody"):
    tds = tr.findall(D + "td")
    if len(tds) < 3:
        continue
    olink = tds[2].find(".//" + D + "olink")
    uid = text(tds[1])
    if olink is None or not re.fullmatch(r"[0-9.]+", uid):
        continue
    sop_classes.append((uid, text(tds[0]), olink.get("targetptr")))

# --------------------------------------------------------------- IOD modules

USAGE = {"M": "Mandatory", "C": "Conditional", "U": "UserOption"}


def iod_table(section_id):
    sec = ids3.get(section_id)
    if sec is None:
        return None, None
    tab = first_table(sec, lambda c: c.endswith("IOD Modules"))
    return sec, tab


def condition_text(s):
    s = re.sub(r"^[CU]\s*-\s*", "", s)
    return s.strip()


iods = {}  # section label -> (name, table label, [(module name, table id, usage, condition)])
functional_groups = {}  # section label -> (table label, [(macro name, table id, usage as written)])
wanted_tables = set()
for uid, name, target in sop_classes:
    sec, tab = iod_table(target)
    if tab is None:
        continue
    label = label_of(sec)
    if label in iods:
        continue
    modules = []
    for tr in tab.find(D + "tbody"):
        tds = tr.findall(D + "td")
        if len(tds) < 3:
            continue
        mod, ref, usage = tds[-3], tds[-2], tds[-1]
        xref = ref.find(".//" + D + "xref")
        if xref is None:
            continue
        msec = ids3.get(xref.get("linkend"))
        if msec is None:
            continue
        mtab = msec if msec.tag == D + "table" else first_table(
            msec, lambda c: "Attributes" in c or "Module" in c)
        if mtab is None:
            continue
        u = text(usage)
        modules.append((text(mod), mtab.get(XID), USAGE.get(u[:1], "UserOption"),
                        condition_text(u) if u[:1] == "C" else ""))
        wanted_tables.add(mtab.get(XID))
    caption = text(tab.find(D + "caption"))
    iods[label] = (caption[: -len(" IOD Modules")], tab.get("label"), modules)

    # An enhanced multi-frame IOD names, in a second table, the Functional
    # Group Macros its Shared and Per-Frame Functional Groups Sequences hold
    # (PS3.3 A.1.3, C.7.6.16): the macro, the section that defines it, and
    # the usage, which may also say where the macro may be used.
    fgtab = first_table(sec, lambda c: c.endswith("Functional Group Macros"))
    if fgtab is None:
        continue
    groups = []
    for tr in fgtab.find(D + "tbody"):
        tds = tr.findall(D + "td")
        if len(tds) < 3:
            continue
        xref = tds[1].find(".//" + D + "xref")
        msec = ids3.get(xref.get("linkend")) if xref is not None else None
        if msec is None:
            continue
        mtab = msec if msec.tag == D + "table" else first_table(msec, lambda c: "Attributes" in c)
        if mtab is None:
            continue
        groups.append((text(tds[0]), mtab.get(XID), text(tds[2])))
        wanted_tables.add(mtab.get(XID))
    functional_groups[label] = (fgtab.get("label"), groups)

# ------------------------------------------------------------ attribute rows

TAG = re.compile(r"\(([0-9A-Fa-fxX]{4}),([0-9A-Fa-fxX]{4})\)")
TYPE = re.compile(r"^(1C|2C|1|2|3)\b")
LIST_TITLE = re.compile(r"^(Enumerated Values|Defined Terms)(?: for Value (\d+))?:$")
ITEMS = [
    (re.compile(r"Only a single Item (shall be included|is permitted)", re.I), (1, 1)),
    (re.compile(r"Exactly one Item shall be included", re.I), (1, 1)),
    (re.compile(r"One or more Items (shall|may) be included", re.I), (1, 0)),
    (re.compile(r"Two or more Items shall be included", re.I), (2, 0)),
    (re.compile(r"Zero or more Items (shall|may) be included", re.I), (0, 0)),
    (re.compile(r"Zero or one Items? shall be included", re.I), (0, 1)),
]


def value_lists(container, direct_only=False):
    """{value number: (enumerated?, [terms])} from the variablelists in
    container, or None for a value given two lists."""
    found, seen = {}, set()
    lists = container.iter(D + "variablelist") if not direct_only else [
        v for v in container.iter(D + "variablelist") if section_of(v) is container]
    for vl in lists:
        m = LIST_TITLE.match(text(vl.find(D + "title")))
        if not m:
            continue
        n = int(m.group(2) or 0)
        if n in seen:
            found[n] = None
            continue
        seen.add(n)
        terms = [text(t) for t in vl.iter(D + "term")]
        terms = [t for t in terms if t]
        if terms:
            found[n] = (m.group(1) == "Enumerated Values", terms)
    return {k: v for k, v in found.items() if v is not None}


ADDITIVE = re.compile(r"plus the following|in addition to", re.I)


def extended(container, lists, direct_only):
    """lists, completed when the container says they add to terms specified
    elsewhere ("shall be those specified in C.7.3.1.1.2, plus the
    following"): the terms of the section named are added, and the lists
    become Defined Terms. Empty when that section cannot be read, since half
    a list would refuse values the standard allows."""
    paras = [p for p in container.iter(D + "para")
             if not direct_only or section_of(p) is container]
    additive = [p for p in paras if ADDITIVE.search(text(p))]
    if not additive:
        return lists
    for p in additive:
        for x in p.iter(D + "xref"):
            base = ids3.get(x.get("linkend") or "")
            if base is None or base.tag != D + "section":
                continue
            more = value_lists(base, direct_only=True)
            if not more:
                continue
            merged = {}
            for n in set(lists) | set(more):
                terms = (lists.get(n, (False, []))[1] + more.get(n, (False, []))[1])
                merged[n] = (False, list(dict.fromkeys(terms)))
            return merged
    return {}


def specialization(cell, name):
    """The value lists of the section the description names, when that
    section is about this attribute alone."""
    for x in cell.iter(D + "xref"):
        sec = ids3.get(x.get("linkend") or "")
        if sec is None or sec.tag != D + "section":
            continue
        if text(sec.find(D + "title")).lower() != name.lower():
            continue
        lists = extended(sec, value_lists(sec, direct_only=True), True)
        if lists:
            return lists, label_of(sec)
    return {}, ""


def condition_sentence(desc):
    m = re.search(r"((?:Required|Shall be present) (?:if|only if|when)[^.]*(?:\.\d[^.]*)*\.?)", desc)
    if not m:
        return ""
    s = m.group(1)
    if len(s) > 300:
        s = s[:300].rsplit(" ", 1)[0] + " ..."
    return s


def own_paragraphs(cell):
    """The text of a cell's own paragraphs, without its notes and lists,
    which are informative or list values."""
    out = []
    for p in cell.iter(D + "para"):
        a, skip = parent.get(p), False
        while a is not None and a is not cell:
            if a.tag in (D + "note", D + "variablelist", D + "itemizedlist", D + "orderedlist", D + "table"):
                skip = True
                break
            a = parent.get(a)
        if not skip:
            out.append(text(p))
    return out


tables = {}
pending = list(wanted_tables)
while pending:
    tid = pending.pop()
    if tid in tables or tid not in ids3:
        continue
    tab = ids3[tid]
    rows = []
    for tr in tab.find(D + "tbody") if tab.find(D + "tbody") is not None else []:
        tds = tr.findall(D + "td")
        if not tds:
            continue
        first = text(tds[0])
        depth = len(first) - len(first.lstrip(">"))
        name = first.lstrip(">").strip()
        if name.startswith("Include"):
            xref = tds[0].find(".//" + D + "xref")
            target = xref.get("linkend") if xref is not None else ""
            tgt = ids3.get(target)
            if tgt is not None and tgt.tag != D + "table":
                t2 = first_table(tgt, lambda c: True)
                target = t2.get(XID) if t2 is not None else ""
            if target:
                # An inclusion that holds only under a condition says so in
                # its own cell ("Include ... if and only if Value Type
                # (0040,A040) is NUM") or in the description beside it. A
                # description naming a context group is not a condition.
                cond = ""
                for said in (name[len("Include"):].strip(), text(tds[-1]) if len(tds) > 1 else ""):
                    if re.match(r"(Include )?(Required )?(only )?if\b", said, re.I):
                        cond = said
                        break
                rows.append(("include", depth, target, cond[:300]))
                pending.append(target)
            continue
        if len(tds) < 4:
            continue
        m = TAG.search(text(tds[1]))
        t = TYPE.match(text(tds[2]))
        if not m or not t:
            continue
        desc = text(tds[3])
        lists = extended(tds[3], value_lists(tds[3]), False) if value_lists(tds[3]) else {}
        source = ""
        if not lists:
            lists, source = specialization(tds[3], name)
        items = (0, 0, False)
        for rx, (lo, hi) in ITEMS:
            if rx.search(desc):
                items = (lo, hi, True)
                break
        conditional = t.group(1) in ("1C", "2C")
        cond = condition_sentence(desc) if conditional else ""
        rows.append(("attr", depth, m.group(1).upper() + m.group(2).upper(), name, t.group(1),
                     cond, lists, source, items, own_paragraphs(tds[3]) if conditional else None))
    tables[tid] = (tab.get("label"), text(tab.find(D + "caption")), label_of(section_of(tab)), rows)

# ---------------------------------------------------------------- conditions
#
# A condition is read as data only when the whole of what the description
# says about presence is one sentence of a form below, naming one attribute,
# optionally followed by "May be present otherwise." Anything else, including
# a sentence that matches a form only in part, stays prose: it is kept as
# text and the checker reports it as not checked.

# Names each tag has in the tables generated, for refusing a condition whose
# name and tag do not agree.
names_by_tag = {}
for _label, _caption, _section, _rows in tables.values():
    for r in _rows:
        if r[0] == "attr":
            names_by_tag.setdefault(r[2], set()).add(r[3].lower())

SENTENCE_END = re.compile(r"(?<=\.)\s+(?=[A-Z\"])")
# A sentence other than the condition that says anything about presence
# makes the description prose: it may qualify the condition.
ABOUT_PRESENCE = re.compile(r"\b(required|present|absent|otherwise|included if|shall not be|only if)\b", re.I)
OTHERWISE = "May be present otherwise."
VALUE = r'(?:"[^"]+"|[A-Z0-9][A-Z0-9_]*(?: [A-Z0-9][A-Z0-9_]*)*)'
VALUES = rf"(?:either )?(?P<values>{VALUE}(?:, {VALUE})*(?:,? or {VALUE})?)"
SUBJECT = r"(?:the Value of |the value of )?(?P<name>[A-Z][^()]*?)(?: \((?P<tag>[0-9A-F]{4}),(?P<elem>[0-9A-F]{4})\))?"
FORMS = [
    ("PresentWithValue", re.compile(
        rf"Required if {SUBJECT} is present (?:and has a [Vv]alue of|with a [Vv]alue of|with [Vv]alue) {VALUES}\.?")),
    ("Present", re.compile(rf"Required if {SUBJECT} is present\.?")),
    ("Absent", re.compile(rf"Required if {SUBJECT} is (?:absent|not present)\.?")),
    ("HasValue", re.compile(
        rf"Required if {SUBJECT}(?: Value (?P<n>[1-9]))? (?:has a [Vv]alue of|has the [Vv]alue|equals|is equal to|is) {VALUES}\.?")),
]


def sentences(paragraphs):
    out = []
    for p in paragraphs:
        p = re.sub(r"\.\.+", ".", p.strip())
        # "Required if ..., may be present otherwise." says what the two
        # sentences say; written so in many Functional Group Macro tables.
        p = re.sub(r", may be present otherwise(?:\.|$)", ". " + OTHERWISE, p)
        out += [s.strip() for s in SENTENCE_END.split(p) if s.strip()]
    return out


def parse_condition(paragraphs, siblings):
    """(form, condition) for a condition of a recognized form, or (reason,
    None). siblings are the names and tags of the rows of the table at the
    attribute's own depth, for a condition that names an attribute without
    its tag."""
    said = sentences(paragraphs)
    required = [s for s in said if s.startswith("Required if ")]
    if len(required) != 1:
        return ("no condition sentence" if not required else "two condition sentences"), None
    otherwise = False
    for s in said:
        if s is required[0]:
            continue
        if s == OTHERWISE:
            otherwise = True
        elif ABOUT_PRESENCE.search(s):
            return "another sentence about presence", None
    for form, rx in FORMS:
        m = rx.fullmatch(required[0])
        if not m:
            continue
        name = m.group("name")
        if m.group("tag"):
            tg = m.group("tag") + m.group("elem")
            if name.lower() not in names_by_tag.get(tg, set()):
                return "the name and the tag do not agree", None
        else:
            matches = [t for n, t in siblings if n == name]
            if len(matches) != 1:
                return "names an attribute without its tag", None
            tg = matches[0]
        values = []
        if "values" in m.groupdict() and m.group("values"):
            v = re.sub(r"^either ", "", m.group("values"))
            values = [x.strip().strip('"') for x in re.split(r",? or |, ", v)]
        n = int(m.groupdict().get("n") or 0)
        return form, {"test": form, "tag": tg, "value": n, "values": values, "otherwise": otherwise}
    return "no form matches", None


condition_forms = {}  # form or reason -> count, for the numbers the generator reports


def count(kind, form):
    condition_forms.setdefault(kind, {}).setdefault(form, 0)
    condition_forms[kind][form] += 1


for label, caption, section, rows in tables.values():
    for i, r in enumerate(rows):
        if r[0] != "attr" or r[9] is None:
            continue
        siblings = [(o[3], o[2]) for o in rows if o[0] == "attr" and o[1] == r[1] and "X" not in o[2]]
        form, cond = parse_condition(r[9], siblings)
        count("attribute", form if cond else "prose: " + form)
        rows[i] = r[:9] + (cond,)

module_conditions = {}  # (IOD label, row) -> condition
for label, (name, tlabel, modules) in iods.items():
    for j, (mname, tid, usage, cond) in enumerate(modules):
        if usage != "Conditional":
            continue
        form, parsed = parse_condition([cond], [])
        count("module", form if parsed else "prose: " + form)
        module_conditions[(label, j)] = parsed

# Where a Functional Group Macro may be, when the usage says ("M - May not be
# used as a Shared Functional Group."). Said in no other words is said
# nowhere: the macro may be in either.
PER_FRAME_ONLY = re.compile(r"(?:may|shall) not be used as a shared functional group", re.I)
SHARED_ONLY = re.compile(r"may not be used as a per-frame functional group|shall be used as a shared functional group", re.I)
PLACEMENT = re.compile(r"[;.,]?\s*(?:may|shall) (?:not )?be used as an? (?:shared|per-frame) functional group"
                       r"(?: \(see for specialization\))?\.?", re.I)
fg_rows = {}  # IOD label -> [(macro, table id, usage, condition text, condition, placement)]
for label, (tlabel, groups) in functional_groups.items():
    out_rows = []
    for macro, tid, said in groups:
        m = re.match(r"^\s*([MCU])\s*[-–]?\s*", said)
        usage = USAGE.get(m.group(1) if m else "", "UserOption")
        placement = "Either"
        if PER_FRAME_ONLY.search(said):
            placement = "PerFrameOnly"
        elif SHARED_ONLY.search(said):
            placement = "SharedOnly"
        rest = PLACEMENT.sub("", said[m.end():] if m else said).strip()
        cond, parsed = "", None
        if usage == "Conditional":
            cond = rest
            form, parsed = parse_condition([rest], [])
            count("functional group", form if parsed else "prose: " + form)
        out_rows.append((macro, tid, usage, cond, parsed, placement))
    fg_rows[label] = (tlabel, out_rows)

# -------------------------------------------------------------------- output


def q(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def tag_code(t):
    if "X" in t:
        value = int(t.replace("X", "0"), 16)
        mask = int("".join("0" if c == "X" else "F" for c in t), 16)
        return f"0x{value:08X}, 0x{mask:08X}"
    return f"0x{int(t, 16):08X}, 0xFFFFFFFF"


PLACEMENT_NAMES = {"Either": "SharedOrPerFrame", "SharedOnly": "SharedOnly", "PerFrameOnly": "PerFrameOnly"}
TEST_NAMES = {"Present": "IsPresent", "Absent": "IsAbsent", "HasValue": "HasValue",
              "PresentWithValue": "IsPresentWithValue"}


def condition_code(c):
    if not c:
        return "nil"
    fields = [f"Test: {TEST_NAMES[c['test']]}", f"Tag: 0x{int(c['tag'], 16):08X}"]
    if c["value"]:
        fields.append(f"Value: {c['value']}")
    if c["values"]:
        fields.append(f"Values: []string{{{', '.join(q(v) for v in c['values'])}}}")
    if c["otherwise"]:
        fields.append("Otherwise: true")
    return "&Condition{" + ", ".join(fields) + "}"


out = [f"""// Code generated by scripts/iod-tables.py from DICOM PS3.3 {edition3} and PS3.4
// {edition4} (the DocBook XML NEMA publishes). DO NOT EDIT.

package validate

// standardEdition is the edition of PS3.3 the tables are generated from.
const standardEdition = {q(edition3)}

// sopClassIODs names the IOD of each Storage SOP class, by the section of PS3.3
// that defines it (PS3.4 Table B.5-1). {len(sop_classes)} SOP classes; those whose IOD
// has no table here are left out.
var sopClassIODs = map[string]string{{"""]
for uid, name, target in sop_classes:
    sec = ids3.get(target)
    if sec is not None and label_of(sec) in iods:
        out.append(f"\t{q(uid)}: {q(label_of(sec))}, // {name}")
    else:
        sec2, tab = iod_table(target)
        if tab is not None and label_of(sec2) in iods:
            out.append(f"\t{q(uid)}: {q(label_of(sec2))}, // {name}")
out.append("}\n")

out.append(f"// iods are the IOD module tables of PS3.3 Annex A: {len(iods)} IODs.")
out.append("var iods = map[string]*IOD{")
for label in sorted(iods):
    name, tlabel, modules = iods[label]
    out.append(f"\t{q(label)}: {{Name: {q(name)}, Section: {q(label)}, Table: {q(tlabel)}, Modules: []ModuleUsage{{")
    for j, (mname, tid, usage, cond) in enumerate(modules):
        when = condition_code(module_conditions.get((label, j)))
        out.append(f"\t\t{{{q(mname)}, {q(tables[tid][0] if tid in tables else '')}, {usage}, {q(cond)}, {when}}},")
    if label not in fg_rows:
        out.append("\t}},")
        continue
    fglabel, rows = fg_rows[label]
    out.append(f"\t}}, FunctionalGroupsTable: {q(fglabel)}, FunctionalGroups: []FunctionalGroup{{")
    for macro, tid, usage, cond, parsed, placement in rows:
        out.append(f"\t\t{{{q(macro)}, {q(tables[tid][0] if tid in tables else '')}, {usage}, {q(cond)}, "
                   f"{condition_code(parsed)}, {PLACEMENT_NAMES[placement]}}},")
    out.append("\t}},")
out.append("}\n")

out.append(f"// tables are the module and macro attribute tables of PS3.3, by label: {len(tables)} tables.")
out.append("var tables = map[string]*Table{")
for tid in sorted(tables, key=lambda k: tables[k][0] or ""):
    label, caption, section, rows = tables[tid]
    if not label:
        continue
    out.append(f"\t{q(label)}: {{Name: {q(caption)}, Section: {q(section)}, Rows: []Row{{")
    for r in rows:
        if r[0] == "include":
            _, depth, target, cond = r
            tl = tables.get(target, (None,))[0]
            if tl:
                c = f", Condition: {q(cond)}" if cond else ""
                out.append(f"\t\t{{Depth: {depth}, Include: {q(tl)}{c}}},")
            continue
        _, depth, tg, name, typ, cond, lists, source, items, when = r
        fields = [f"Depth: {depth}" if depth else "", f"Tag: {tag_code(tg).split(',')[0]}",
                  f"Mask: {tag_code(tg).split(', ')[1]}" if "X" in tg else "",
                  f"Name: {q(name)}", f"Type: Type{typ}"]
        if cond:
            fields.append(f"Condition: {q(cond)}")
        if lists:
            vs = []
            for n in sorted(lists):
                enum, terms = lists[n]
                vs.append(f"{{Value: {n}, Enumerated: {'true' if enum else 'false'}, Terms: []string{{{', '.join(q(x) for x in terms)}}}}}")
            fields.append(f"Values: []ValueList{{{', '.join(vs)}}}")
            if source:
                fields.append(f"ValuesFrom: {q(source)}")
        if items[2]:
            fields.append(f"Items: &ItemCount{{Min: {items[0]}, Max: {items[1]}}}")
        if when:
            fields.append(f"When: {condition_code(when)}")
        out.append("\t\t{" + ", ".join(f for f in fields if f) + "},")
    out.append("\t}},")
out.append("}")
print("\n".join(out))
print(f"PS3.3 {edition3}: {len(sop_classes)} SOP classes, {len(iods)} IODs, {len(tables)} tables",
      file=sys.stderr)
for kind in ("attribute", "module", "functional group"):
    forms = condition_forms.get(kind, {})
    print(f"{kind} conditions: {sum(forms.values())}", file=sys.stderr)
    for form, n in sorted(forms.items(), key=lambda kv: (kv[0].startswith("prose"), -kv[1])):
        print(f"  {form}: {n}", file=sys.stderr)
