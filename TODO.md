# Gnoma ELF Support - TODO List

## Overview
This document outlines the steps to add **ELF (Executable and Linkable Format)** support to Gnoma, enabling features like ELF parsing, disassembly, security analysis, and binary manipulation.

---

## 📌 Goals
- Add ELF-specific tools to Gnoma’s toolset.
- Enable users to analyze, disassemble, and manipulate ELF binaries.
- Integrate with Gnoma’s existing permission and security systems.

---

## ✅ Implementation Steps

### 1. **Design ELF Tools**
- [ ] **`elf.parse`**: Parse and display ELF headers, sections, and segments.
- [ ] **`elf.disassemble`**: Disassemble code sections (e.g., `.text`) using `objdump` or a pure-Go disassembler.
- [ ] **`elf.analyze`**: Perform security analysis (e.g., check for packed binaries, missing security flags).
- [ ] **`elf.patch`**: Modify binary bytes or inject code (advanced feature).
- [ ] **`elf.symbols`**: Extract and list symbols from the symbol table.

### 2. **Implement ELF Tools**
- [ ] Create a new package: `internal/tool/elf`.
- [ ] Implement each tool as a struct with `Name()` and `Run(args map[string]interface{})` methods.
- [ ] Use `debug/elf` (standard library) or third-party libraries like `github.com/xyproto/elf` for parsing.
- [ ] Add support for external tools like `objdump` and `radare2` for disassembly.

#### Example: `elf.Parse` Tool
```go
package elf

import (
    "debug/elf"
    "fmt"
    "os"
)

type ParseTool struct{}

func NewParseTool() *ParseTool {
    return &ParseTool{}
}

func (t *ParseTool) Name() string {
    return "elf.parse"
}

func (t *ParseTool) Run(args map[string]interface{}) (string, error) {
    filePath, ok := args["file"].(string)
    if !ok {
        return "", fmt.Errorf("missing 'file' argument")
    }

    f, err := os.Open(filePath)
    if err != nil {
        return "", fmt.Errorf("failed to open file: %v", err)
    }
    defer f.Close()

    ef, err := elf.NewFile(f)
    if err != nil {
        return "", fmt.Errorf("failed to parse ELF: %v", err)
    }
    defer ef.Close()

    // Extract and format ELF headers
    output := fmt.Sprintf("ELF Header:\n%s\n", ef.FileHeader)
    output += fmt.Sprintf("Sections:\n")
    for _, s := range ef.Sections {
        output += fmt.Sprintf("  - %s (size: %d)\n", s.Name, s.Size)
    }
    output += fmt.Sprintf("Program Headers:\n")
    for _, p := range ef.Progs {
        output += fmt.Sprintf("  - Type: %s, Offset: %d, Vaddr: %x\n", p.Type, p.Off, p.Vaddr)
    }

    return output, nil
}
```

### 3. **Integrate ELF Tools with Gnoma**
- [ ] Update `buildToolRegistry()` in `cmd/gnoma/main.go` to register ELF tools:
  ```go
  func buildToolRegistry() *tool.Registry {
      reg := tool.NewRegistry()
      reg.Register(bash.New())
      reg.Register(fs.NewReadTool())
      reg.Register(fs.NewWriteTool())
      reg.Register(fs.NewEditTool())
      reg.Register(fs.NewGlobTool())
      reg.Register(fs.NewGrepTool())
      reg.Register(fs.NewLSTool())
      reg.Register(elf.NewParseTool())      // New ELF tool
      reg.Register(elf.NewDisassembleTool()) // New ELF tool
      reg.Register(elf.NewAnalyzeTool())    // New ELF tool
      return reg
  }
  ```

### 4. **Add Documentation**
- [ ] Add usage examples to `docs/elf-tools.md`.
- [ ] Update `CLAUDE.md` with ELF tool capabilities.

### 5. **Testing**
- [ ] Test ELF tools on sample binaries (e.g., `/bin/ls`, `/bin/bash`).
- [ ] Test edge cases (e.g., stripped binaries, packed binaries).
- [ ] Ensure integration with Gnoma’s permission and security systems.

### 6. **Security Considerations**
- [ ] Sandbox ELF tools to prevent malicious binaries from compromising the system.
- [ ] Validate file paths and arguments to avoid directory traversal or arbitrary file writes.
- [ ] Use Gnoma’s firewall to scan ELF tool outputs for suspicious patterns.

---

## 🛠️ Dependencies
- **Go Libraries**:
  - [`debug/elf`](https://pkg.go.dev/debug/elf) (standard library).
  - [`github.com/xyproto/elf`](https://github.com/xyproto/elf) (third-party).
  - [`github.com/anchore/go-elf`](https://github.com/anchore/go-elf) (third-party).
- **External Tools**:
  - `objdump` (for disassembly).
  - `readelf` (for detailed ELF analysis).
  - `radare2` (for advanced reverse engineering).

---

## 📝 Example Usage
### Interactive Mode
```
> Use the elf.parse tool to analyze /bin/ls
> elf.parse --file /bin/ls
```

### Pipe Mode
```bash
echo '{"file": "/bin/ls"}' | gnoma --tool elf.parse
```

---

## 🚀 Future Enhancements
- Add support for **PE (Portable Executable)** and **Mach-O** formats.
- Integrate with **Ghidra** or **IDA Pro** for advanced analysis.
- Add **automated exploit detection** for binaries.
