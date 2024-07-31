package common

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewConfigMapBuilder(metadata *metav1.ObjectMeta) *ConfigMapBuilder {
	if metadata == nil {
		panic("metadata is nil")
	}
	return &ConfigMapBuilder{
		metadata: metadata,
	}
}

type ConfigMapBuilder struct {
	metadata *metav1.ObjectMeta
	data     *map[string]string
}

// AddData add data
func (b *ConfigMapBuilder) AddData(key string, value string) *ConfigMapBuilder {
	if b.data == nil {
		b.data = &map[string]string{}
	}
	(*b.data)[key] = value
	return b
}

// SetData set Data
func (b *ConfigMapBuilder) SetData(data map[string]string) *ConfigMapBuilder {
	b.data = &data
	return b
}

func (b *ConfigMapBuilder) Build() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: *b.metadata,
		Data:       *b.data,
	}
}
