package cmd_test

import (
	"os"

	"github.com/openshift-kni/cluster-group-upgrades-operator/recovery/cmd"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LaunchBackup", func() {
	Describe("ParseBackupPath", func() {
		Context("When BackupPath contains a slash", func() {
			It("tests strip slash", func() {
				BackupPath := cmd.ParseBackupPath("foo/")
				Expect(BackupPath).To(Equal("foo"))
			})
		})

		Context("When BackupPath contains no slash", func() {
			It("tests return normal value", func() {
				BackupPath := cmd.ParseBackupPath("foo")
				Expect(BackupPath).To(Equal("foo"))
			})
		})
	})

	Describe("RecoveryInProgress", func() {
		Context("progressfile exists", func() {
			It("returns false", func() {
				progressFile := "/tmp/progressfile"
				_, err := os.OpenFile(progressFile, os.O_RDONLY|os.O_CREATE, 0755)
				if err != nil {
					return
				}
				result := cmd.RecoveryInProgress(progressFile)
				Expect(result).To(Equal(true))
				err = os.Remove(progressFile)
				if err != nil {
					return
				}
			})

		})

		Context("progressfile doesn't exist", func() {
			It("returns true", func() {
				result := cmd.RecoveryInProgress("/tmp/progressfile")
				Expect(result).To(Equal(false))
			})
		})
	})

	Describe("Cleanup", func() {
		Context("cleans up files", func() {
			It("cleanup with dir", func() {
				dir, _ := os.MkdirTemp("", "tmpDir")
				err := cmd.Cleanup(dir)
				defer func(path string) {
					err := os.RemoveAll(path)
					if err != nil {
						return
					}
				}(dir)
				Expect(err).Should(BeNil())
			})

			It("cleanup with dir and file", func() {
				dir, _ := os.MkdirTemp("", "tmpDir")
				_, err := os.OpenFile(dir+"/foo", os.O_RDONLY|os.O_CREATE, 0755)
				if err != nil {
					return
				}
				defer func(path string) {
					err := os.RemoveAll(path)
					if err != nil {
						return
					}
				}(dir)
				err = cmd.Cleanup("dir")
				Expect(err).Should(BeNil())
			})
		})
	})

	Describe("ExecuteCmd", func() {
		Context("execute command", func() {
			It("command succeeds", func() {
				err := cmd.ExecuteCmd(":")
				Expect(err).Should(BeNil())
			})

			It("command fails", func() {
				err := cmd.ExecuteCmd("foo")
				Expect(err).To(HaveOccurred())
				Expect(err).Should(Not(BeNil()))
			})
		})
	})
})
