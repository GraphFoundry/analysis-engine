---
name: "Post-Change Verification Audit"
description: "Comprehensive checklist to verify changes comply with governance policies"
---

# Post-Change Verification Audit

Run this audit after making any code changes to ensure compliance with repository governance.

---

## When to Use

Run this verification:
- After implementing new features
- After fixing bugs
- After refactoring code
- Before creating pull requests
- When reviewing changes from others

---

## Verification Checklist

### 1) Architecture Compliance

#### Graph Engine Single Source
```bash
# Verify no Neo4j references
git grep -n -i "neo4j\|bolt\|cypher\|neo4j-driver"

# Verify no fallback logic
git grep -n -E "fallback|alternative.*graph|if.*neo4j"

# Verify Graph Engine client usage
git grep -n "graphEngineClient\|GraphEngineHttpProvider"
```

**Expected results:**
- ✅ Zero Neo4j references
- ✅ Zero fallback patterns
- ✅ Graph Engine client used for all graph data access

#### External Service Resilience
```bash
# Verify timeouts are set
git grep -n "timeout:" src/

# Verify error handling exists
git grep -n "catch (error)" src/

# Verify 503 on service unavailable
git grep -n "503\|SERVICE_UNAVAILABLE"
```

**Expected results:**
- ✅ All HTTP requests have explicit timeouts
- ✅ All external calls have try-catch blocks
- ✅ 503 returned when Graph Engine unavailable

---

### 2) Security & Logging

#### No Credential Exposure
```bash
# Verify redaction in logs
git grep -n "redact\|password\|token" src/

# Check for hardcoded credentials (should be none)
git grep -n -E "password.*=.*['\"]|token.*=.*['\"]"

# Verify env var usage
git grep -n "process.env"
```

**Expected results:**
- ✅ Credentials redacted in logs
- ✅ No hardcoded passwords/tokens
- ✅ Secrets loaded from environment variables

---

### 3) API Contract Consistency

#### OpenAPI Synchronization
```bash
# List modified endpoint files
git diff --name-only HEAD | grep -E "index.js|routes/"

# Check if openapi.yaml was updated
git diff --name-only HEAD | grep openapi.yaml

# Verify version bump
git diff openapi.yaml | grep "version:"
```

**Required if endpoints changed:**
- ✅ `openapi.yaml` updated
- ✅ Version bumped (patch or minor)
- ✅ Request/response schemas match code

**Validation (if Swagger enabled):**
```bash
ENABLE_SWAGGER=true npm start &
sleep 2
curl http://localhost:3000/swagger | grep "swagger"
kill %1
```

---

### 4) Testing Coverage

#### Test Files Updated
```bash
# Check if tests were added/updated
git diff --name-only HEAD | grep "test/"

# Run tests
npm test

# Check test coverage (if available)
npm run test:coverage
```

**Expected results:**
- ✅ Tests exist for new/modified behavior
- ✅ All tests passing
- ✅ Coverage maintained or improved

#### Test Scenarios Covered
For behavioral changes, verify tests cover:
- [ ] Happy path (success case)
- [ ] Graph Engine unavailable (503)
- [ ] Timeout scenario
- [ ] Invalid input (400)
- [ ] Upstream error (502)

---

### 5) Documentation Synchronization

#### Docs Updated
```bash
# Check for modified docs
git diff --name-only HEAD | grep -E "\.md$|docs/"

# Verify docs mention new features
git diff README.md DEPLOYMENT.md docs/
```

**Required updates when:**
- New endpoint added → Update README.md
- New env var required → Update DEPLOYMENT.md
- New workflow → Update docs/COPILOT-USAGE-GUIDE.md
- Policy change → Update .github/copilot-instructions.md

---

### 6) Governance File Consistency

#### Instruction/Prompt/Skill Files
```bash
# Check for broken references
git grep -n "\.github/.*\.md" .github/

# Verify no orphaned references
git grep -n -E "neo4j-readonly|04-neo4j-fallback" .github/

# Check file structure
ls -la .github/instructions/
ls -la .github/prompts/
ls -la .github/skills/
```

**Expected results:**
- ✅ No references to removed files
- ✅ All referenced files exist
- ✅ File numbering is sequential (instructions/prompts)

---

### 7) Code Quality

#### Linting & Formatting
```bash
# Run linter (if configured)
npm run lint

# Check for console.log (should use logger)
git grep -n "console\\.log" src/

# Check for TODO/FIXME comments
git grep -n "TODO\|FIXME" src/
```

**Expected results:**
- ✅ No linting errors
- ✅ No console.log in production code (use logger)
- ✅ TODOs tracked or removed

---

## Regression Scan Commands

### Quick Scan (Essential)
```bash
#!/bin/bash
echo "=== Regression Scan ==="

echo "1. Neo4j references..."
git grep -n -i "neo4j\|bolt\|cypher" && echo "❌ FAIL" || echo "✅ PASS"

echo "2. Fallback logic..."
git grep -n -i "fallback" src/ && echo "❌ FAIL" || echo "✅ PASS"

echo "3. Tests..."
npm test && echo "✅ PASS" || echo "❌ FAIL"

echo "4. OpenAPI sync..."
git diff --name-only HEAD | grep -E "index.js|routes/" >/dev/null && \
  git diff --name-only HEAD | grep openapi.yaml >/dev/null && \
  echo "✅ PASS" || echo "⚠️  WARNING: Endpoints changed but openapi.yaml not updated"
```

### Full Audit (Comprehensive)
```bash
#!/bin/bash
echo "=== Full Governance Audit ==="

# Architecture
echo "Architecture Compliance:"
echo "- Neo4j references: $(git grep -c -i 'neo4j' 2>/dev/null || echo 0)"
echo "- Fallback patterns: $(git grep -c -i 'fallback' src/ 2>/dev/null || echo 0)"
echo "- Graph Engine usage: $(git grep -c 'graphEngineClient' src/ 2>/dev/null || echo 0)"

# Security
echo "Security Checks:"
echo "- Credential redaction: $(git grep -c 'redact' src/ 2>/dev/null || echo 0)"
echo "- Hardcoded secrets: $(git grep -c -E "password.*=.*['\"]" src/ 2>/dev/null || echo 0)"

# Testing
echo "Testing:"
npm test 2>&1 | grep -E "passing|failing"

# Docs
echo "Documentation:"
echo "- Modified docs: $(git diff --name-only HEAD | grep -c '\.md$' || echo 0)"

# Governance
echo "Governance Files:"
echo "- Broken refs: $(git grep -c -E "neo4j-readonly|04-neo4j-fallback" .github/ 2>/dev/null || echo 0)"
```

---

## Automated Verification Script

Save as `scripts/verify-changes.sh`:

```bash
#!/bin/bash
set -e

echo "🔍 Running post-change verification..."

# 1. Architecture
echo "📋 Checking architecture compliance..."
if git grep -q -i "neo4j\|bolt\|cypher"; then
  echo "❌ Neo4j references found!"
  git grep -n -i "neo4j\|bolt\|cypher"
  exit 1
fi

if git grep -q -i "fallback" src/; then
  echo "⚠️  Fallback logic detected - verify compliance"
  git grep -n -i "fallback" src/
fi

# 2. Tests
echo "🧪 Running tests..."
npm test

# 3. OpenAPI sync
if git diff --name-only HEAD | grep -q -E "index.js|routes/"; then
  if ! git diff --name-only HEAD | grep -q "openapi.yaml"; then
    echo "⚠️  WARNING: Endpoints changed but openapi.yaml not updated"
    echo "   See .github/copilot-instructions.md §0.4"
  fi
fi

# 4. Security
echo "🔒 Checking security..."
if git grep -q -E "password.*=.*['\"]|token.*=.*['\"]" src/; then
  echo "❌ Hardcoded credentials found!"
  git grep -n -E "password.*=.*['\"]|token.*=.*['\"]" src/
  exit 1
fi

echo "✅ Verification complete!"
```

Make executable:
```bash
chmod +x scripts/verify-changes.sh
```

---

## Pass Criteria

Changes are ready to commit when:

- [x] No Neo4j references exist
- [x] No fallback logic to alternative data sources
- [x] All tests passing
- [x] OpenAPI spec updated (if endpoints changed)
- [x] Documentation updated (if behavior changed)
- [x] No hardcoded credentials
- [x] Timeouts set on all HTTP requests
- [x] Error handling follows standard patterns
- [x] No broken references in .github/ files

---

## Failure Response

If verification fails:

1. **Identify root cause** — Which check failed?
2. **Review policy** — Read relevant .github/instructions/ file
3. **Fix violation** — Update code to comply
4. **Re-run verification** — Ensure all checks pass
5. **Document** — If intentional deviation, document why

---

## Integration with Pull Requests

Add to PR template (`.github/pull_request_template.md`):

```markdown
## Pre-Merge Verification

- [ ] Ran `scripts/verify-changes.sh` (all checks passed)
- [ ] No Neo4j references: `git grep -i neo4j`
- [ ] Tests passing: `npm test`
- [ ] OpenAPI updated (if endpoints changed)
- [ ] Docs updated (if behavior changed)
```

---

## Quick Reference

| Check | Command | Expected |
|-------|---------|----------|
| Neo4j refs | `git grep -i neo4j` | Zero matches |
| Fallback logic | `git grep -i fallback src/` | Zero matches |
| Tests | `npm test` | All passing |
| OpenAPI sync | `git diff openapi.yaml` | Updated if endpoints changed |
| Credentials | `git grep -E "password.*="` | Zero matches |
| Timeouts | `git grep timeout: src/` | Present in all HTTP calls |
