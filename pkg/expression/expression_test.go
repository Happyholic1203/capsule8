// Copyright 2018 Capsule8, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package expression

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressionStruct(t *testing.T) {
	_, err := ConvertExpression(nil, nil)
	assert.Error(t, err)

	types := FieldTypeMap{
		"foo": ValueTypeUnsignedInt8,
	}

	invalidExpr := Equal(Identifier("foo"), Value("string"))
	validExpr := Equal(Identifier("foo"), Value(uint8(8)))

	// Intentional type mismatch uint8 vs. string
	_, err = ConvertExpression(invalidExpr, types)
	assert.Error(t, err)

	expr, err := ConvertExpression(validExpr, types)
	require.NoError(t, err)
	require.NotNil(t, expr)

	assert.Equal(t, "foo = 8", expr.String())
	assert.Equal(t, "foo == 8", expr.KernelFilterString())

	err = expr.ValidateKernelFilter()
	assert.NoError(t, err)

	// Cannot bind more than once
	err = expr.BindTypes(types)
	assert.Error(t, err)

	values := FieldValueMap{
		"foo": uint8(8),
	}
	result, err := expr.Evaluate(values)
	if assert.NoError(t, err) {
		assert.True(t, IsValueTrue(result))
	}

	// Test late binding
	expr, err = ConvertExpression(validExpr, nil)
	require.NoError(t, err)
	require.NotNil(t, expr)

	fmt.Printf("expr: %#v\n", expr)
	assert.Equal(t, "foo = 8", expr.String())
	assert.Equal(t, "foo == 8", expr.KernelFilterString())

	err = expr.ValidateKernelFilter()
	assert.NoError(t, err)

	// Cannot evaluate without binding types
	_, err = expr.Evaluate(values)
	assert.Error(t, err)

	err = expr.BindTypes(types)
	require.NoError(t, err)

	// Cannot bind more than once
	err = expr.BindTypes(types)
	assert.Error(t, err)

	result, err = expr.Evaluate(values)
	if assert.NoError(t, err) {
		assert.True(t, IsValueTrue(result))
	}
}

func TestParseBytes(t *testing.T) {
	_, err := ParseBytes([]byte("u16 == 12"), ParseModeInvalid, parserTestTypes)
	assert.Error(t, err)

	e, err := ParseBytes([]byte("u16 == 12"), ParseModeKernelFilter, parserTestTypes)
	assert.NoError(t, err)
	if assert.NotNil(t, e) {
		assert.Equal(t, "u16 == 12", e.KernelFilterString())
	}
}

func TestParseString(t *testing.T) {
	_, err := ParseString("u16 == 12", ParseModeInvalid, parserTestTypes)
	assert.Error(t, err)

	e, err := ParseString("u16 == 12", ParseModeKernelFilter, parserTestTypes)
	assert.NoError(t, err)
	if assert.NotNil(t, e) {
		assert.Equal(t, "u16 == 12", e.KernelFilterString())
	}
}

func TestParse(t *testing.T) {
	r := bytes.NewReader([]byte("u16 == 12"))
	_, err := Parse(r, ParseModeInvalid, parserTestTypes)
	assert.Error(t, err)

	r = bytes.NewReader([]byte("u16 == 12"))
	e, err := Parse(r, ParseModeKernelFilter, parserTestTypes)
	assert.NoError(t, err)
	if assert.NotNil(t, e) {
		assert.Equal(t, "u16 == 12", e.KernelFilterString())
	}
}

func TestIsValueTrue(t *testing.T) {
	falseTests := []interface{}{
		"",
		int8(0),
		int16(0),
		int32(0),
		int64(0),
		uint8(0),
		uint16(0),
		uint32(0),
		uint64(0),
		false,
		0.0,
		time.Unix(0, 0),
		make(chan interface{}),
	}
	trueTests := []interface{}{
		"string",
		int8(8),
		int16(8),
		int32(8),
		int64(8),
		uint8(8),
		uint16(8),
		uint32(8),
		uint64(8),
		true,
		8.0,
		time.Now(),
	}

	for _, v := range falseTests {
		assert.False(t, IsValueTrue(v), fmt.Sprintf("%#v", v))
	}
	for _, v := range trueTests {
		assert.True(t, IsValueTrue(v), fmt.Sprintf("%#v", v))
	}
}

func TestNewValue(t *testing.T) {
	var v *telemetryAPI.Value

	v = NewValue("string")
	assert.Equal(t, "string", v.GetStringValue())

	v = NewValue(int8(8))
	assert.Equal(t, int64(8), v.GetSignedValue())

	v = NewValue(int16(8))
	assert.Equal(t, int64(8), v.GetSignedValue())

	v = NewValue(int32(8))
	assert.Equal(t, int64(8), v.GetSignedValue())

	v = NewValue(int64(8))
	assert.Equal(t, int64(8), v.GetSignedValue())

	v = NewValue(uint8(8))
	assert.Equal(t, uint64(8), v.GetUnsignedValue())

	v = NewValue(uint16(8))
	assert.Equal(t, uint64(8), v.GetUnsignedValue())

	v = NewValue(uint32(8))
	assert.Equal(t, uint64(8), v.GetUnsignedValue())

	v = NewValue(uint64(8))
	assert.Equal(t, uint64(8), v.GetUnsignedValue())

	v = NewValue(true)
	assert.Equal(t, true, v.GetBoolValue())

	v = NewValue(8.0)
	assert.Equal(t, 8.0, v.GetDoubleValue())

	ts := &timestamp.Timestamp{
		Seconds: 8,
		Nanos:   88,
	}
	v = NewValue(ts)
	ts = v.GetTimestampValue()
	assert.Equal(t, int64(8), ts.Seconds)
	assert.Equal(t, int32(88), ts.Nanos)

	v = NewValue(make(chan interface{}))
	assert.Nil(t, v)
}

func TestExpressionNodes(t *testing.T) {
	var e *telemetryAPI.Expression

	e = Identifier("foo")
	assert.Equal(t, telemetryAPI.Expression_IDENTIFIER, e.GetType())
	assert.Equal(t, "foo", e.GetIdentifier())

	e = Value("foo")
	if assert.Equal(t, telemetryAPI.Expression_VALUE, e.GetType()) {
		assert.Equal(t, "foo", e.GetValue().GetStringValue())
	}

	// Unary ops

	i := Identifier("foo")
	unaryTestCases := []struct {
		t telemetryAPI.Expression_ExpressionType
		f func(*telemetryAPI.Expression) *telemetryAPI.Expression
	}{
		{telemetryAPI.Expression_IS_NULL, IsNull},
		{telemetryAPI.Expression_IS_NOT_NULL, IsNotNull},
	}
	for _, tc := range unaryTestCases {
		testCase := telemetryAPI.Expression_ExpressionType_name[int32(tc.t)]
		e = tc.f(i)
		if assert.Equal(t, tc.t, e.GetType(), testCase) {
			assert.Equal(t, i, e.GetUnaryOp(), testCase)
		}
	}

	// Binary ops

	v := Value("string")
	v2 := Value(int32(8))
	binaryTestCases := []struct {
		t telemetryAPI.Expression_ExpressionType
		f func(*telemetryAPI.Expression, *telemetryAPI.Expression) *telemetryAPI.Expression
		v *telemetryAPI.Expression
	}{
		{telemetryAPI.Expression_EQ, Equal, v},
		{telemetryAPI.Expression_NE, NotEqual, v},
		{telemetryAPI.Expression_LT, LessThan, v},
		{telemetryAPI.Expression_LE, LessThanEqualTo, v},
		{telemetryAPI.Expression_GT, GreaterThan, v},
		{telemetryAPI.Expression_GE, GreaterThanEqualTo, v},
		{telemetryAPI.Expression_LIKE, Like, v},
		{telemetryAPI.Expression_BITWISE_AND, BitwiseAnd, v2},
	}
	for _, tc := range binaryTestCases {
		testCase := telemetryAPI.Expression_ExpressionType_name[int32(tc.t)]
		e = tc.f(i, tc.v)
		if assert.Equal(t, tc.t, e.GetType(), testCase) {
			e2 := e.GetBinaryOp()
			assert.Equal(t, i, e2.GetLhs(), testCase)
			assert.Equal(t, tc.v, e2.GetRhs(), testCase)
		}
	}

	// Do LogicalAnd and LogicalOr last after we know that Equal works
	lhs := Equal(Identifier("foo"), Value(true))
	rhs := Equal(Identifier("bar"), Value(true))
	logicalTestCases := []struct {
		t telemetryAPI.Expression_ExpressionType
		f func(*telemetryAPI.Expression, *telemetryAPI.Expression) *telemetryAPI.Expression
	}{
		{telemetryAPI.Expression_LOGICAL_AND, LogicalAnd},
		{telemetryAPI.Expression_LOGICAL_OR, LogicalOr},
	}
	for _, tc := range logicalTestCases {
		testCase := telemetryAPI.Expression_ExpressionType_name[int32(tc.t)]
		assert.Equal(t, lhs, tc.f(lhs, nil), testCase)
		assert.Equal(t, rhs, tc.f(nil, rhs), testCase)
		e = tc.f(lhs, rhs)
		if assert.Equal(t, tc.t, e.GetType(), testCase) {
			e2 := e.GetBinaryOp()
			assert.Equal(t, lhs, e2.GetLhs(), testCase)
			assert.Equal(t, rhs, e2.GetRhs(), testCase)
		}
	}
}
