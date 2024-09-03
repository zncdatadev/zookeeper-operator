package util_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/zncdatadev/zookeeper-operator/internal/util"
)

var _ = Describe("ToProperties", func() {
	Context("with a non-empty map", func() {
		It("should convert the map to a sorted properties string", func() {
			data := map[string]string{
				"b": "2",
				"a": "1",
				"c": "3",
			}
			result := ToProperties(data)
			expected := "a=1\nb=2\nc=3\n"
			Expect(result).To(Equal(expected))
		})
	})

	Context("with an empty map", func() {
		It("should return an empty string", func() {
			data := map[string]string{}
			result := ToProperties(data)
			Expect(result).To(Equal(""))
		})
	})

	Context("with keys containing special characters", func() {
		It("should handle the special characters correctly", func() {
			data := map[string]string{
				"key!@#":  "value1",
				"key$%^":  "value2",
				"key&*()": "value3",
			}
			result := ToProperties(data)
			expected := "key!@#=value1\nkey$%^=value2\nkey&*()=value3\n"
			Expect(result).To(Equal(expected))
		})
	})
})
