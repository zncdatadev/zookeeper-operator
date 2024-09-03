package server

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StatefulsetBuilder", func() {
	Context("when generating main container command args", func() {
		It("should correctly generate command arguments", func() {
			// given
			b := &StatefulsetBuilder{}

			// when
			args := b.getMainContainerCommanArgs()

			// then
			Expect(args).NotTo(BeNil())
			Expect(args[0]).To(ContainSubstring("bin/zkServer.sh start-foreground /kubedoop/config/zoo.cfg &"))
		})
	})
})
