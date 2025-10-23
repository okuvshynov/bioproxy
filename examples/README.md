# Example Configuration and Templates

This directory contains example configuration and template files to help you get started with bioproxy.

## Files

### config.json
Complete example configuration with:
- Proxy and admin server settings
- llama.cpp backend URL
- Warmup check interval
- Template prefix mappings

### templates/

**code_assistant.txt**
- Simple coding assistant template
- Demonstrates `<{message}>` placeholder usage
- No file inclusions

**debug_helper.txt**
- Debugging expert template
- Demonstrates file inclusion with `<{examples/templates/debugging_guide.txt}>`
- Shows how to combine file inclusion with message placeholder

**debugging_guide.txt**
- Reference documentation file
- Included by debug_helper.txt
- Demonstrates content that gets included in other templates

## Usage

### Quick Start

Run bioproxy with the example configuration:

```bash
./bioproxy -config examples/config.json
```

### Customize

Copy the config to your own location and modify:

```bash
cp examples/config.json config.json
# Edit config.json to customize
./bioproxy -config config.json
```

### Create Your Own Templates

1. Create a template file (e.g., `my_template.txt`)
2. Use `<{message}>` for user message substitution
3. Use `<{filepath}>` to include other files
4. Add mapping to config.json:
   ```json
   "prefixes": {
     "@myprefix": "path/to/my_template.txt"
   }
   ```

## Template Placeholder Reference

**Message placeholder:**
```
<{message}>
```
Replaced with the user's actual message (without the prefix)

**File inclusion:**
```
<{path/to/file.txt}>
```
Replaced with the content of the specified file

**Note:** Placeholders are NOT recursive - patterns in substituted content are not processed.
