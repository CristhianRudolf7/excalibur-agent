package store

import "testing"

func TestParseMountsLongestPrefix(t *testing.T) {
	mounts := parseMounts(`/dev/sda1 / ext4 rw 0 0
nas:/data /mnt/data nfs rw 0 0
//srv/share /mnt/data/sub cifs rw 0 0
`)
	if m := mountForDir(mounts, "/mnt/data/sub/dir"); m == nil || m.fsType != "cifs" {
		t.Fatalf("got %+v, want cifs", m)
	}
	if m := mountForDir(mounts, "/mnt/data/other"); m == nil || m.fsType != "nfs" {
		t.Fatalf("got %+v, want nfs", m)
	}
	if m := mountForDir(mounts, "/home/u"); m == nil || m.fsType != "ext4" {
		t.Fatalf("got %+v, want ext4", m)
	}
}

func TestNetworkFSTypes(t *testing.T) {
	for _, fs := range []string{"nfs", "nfs4", "cifs", "smb", "smbfs", "sshfs", "fuse.sshfs", "9p", "vboxsf"} {
		if !networkFSTypes[fs] {
			t.Fatalf("%s not flagged as network fs", fs)
		}
	}
	for _, fs := range []string{"ext4", "xfs", "btrfs", "tmpfs", "overlay", "fuseblk"} {
		if networkFSTypes[fs] {
			t.Fatalf("%s wrongly flagged as network fs", fs)
		}
	}
}
