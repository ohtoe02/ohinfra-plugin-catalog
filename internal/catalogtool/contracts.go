package catalogtool

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func VerifySHA256SUMS(root string) error {
	encoded, err := os.ReadFile(filepath.Join(root, "SHA256SUMS"))
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(encoded)))
	seen := map[string]struct{}{}
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid SHA256SUMS line %q", line)
		}
		digest, relative := parts[0], filepath.FromSlash(parts[1])
		if decoded, err := hex.DecodeString(digest); err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("invalid SHA-256 for %q", parts[1])
		}
		if filepath.IsAbs(relative) || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe SHA256SUMS path %q", parts[1])
		}
		if _, duplicate := seen[parts[1]]; duplicate {
			return fmt.Errorf("duplicate SHA256SUMS path %q", parts[1])
		}
		seen[parts[1]] = struct{}{}
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("checksummed path %q is not a regular file", parts[1])
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != digest {
			return fmt.Errorf("SHA-256 mismatch for %q", parts[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(seen) == 0 {
		return errors.New("SHA256SUMS contains no files")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		slashRelative := filepath.ToSlash(relative)
		if slashRelative == "SHA256SUMS" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("contract path %q is not a regular file", slashRelative)
		}
		if _, listed := seen[slashRelative]; !listed {
			return fmt.Errorf("contract file %q is not listed in SHA256SUMS", slashRelative)
		}
		return nil
	})
}
