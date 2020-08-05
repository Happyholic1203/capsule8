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
	"testing"
	"time"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"

	"github.com/stretchr/testify/assert"
)

func callConvertValue(value *telemetryAPI.Value) (v *valueExpr, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exprError); ok {
				err = e.error
			} else {
				panic(r)
			}
		}
	}()

	v = convertValue(value).(*valueExpr)
	return
}

func TestConvertValue(t *testing.T) {
	values := map[telemetryAPI.ValueType]interface{}{
		telemetryAPI.ValueType_STRING:    "string",
		telemetryAPI.ValueType_SINT8:     int8(8),
		telemetryAPI.ValueType_SINT16:    int16(8),
		telemetryAPI.ValueType_SINT32:    int32(8),
		telemetryAPI.ValueType_SINT64:    int64(8),
		telemetryAPI.ValueType_UINT8:     uint8(8),
		telemetryAPI.ValueType_UINT16:    uint16(8),
		telemetryAPI.ValueType_UINT32:    uint32(8),
		telemetryAPI.ValueType_UINT64:    uint64(8),
		telemetryAPI.ValueType_BOOL:      true,
		telemetryAPI.ValueType_DOUBLE:    8.0,
		telemetryAPI.ValueType_TIMESTAMP: time.Unix(8, 8),
	}

	for valueType, value := range values {
		testCase := telemetryAPI.ValueType_name[int32(valueType)]

		_, err := callConvertValue(&telemetryAPI.Value{Type: valueType})
		assert.Error(t, err, testCase)

		v, err := callConvertValue(NewValue(value))
		if assert.NoError(t, err, testCase) {
			assert.Equal(t, v.v, value, testCase)
		}
	}

	_, err := callConvertValue(&telemetryAPI.Value{Type: 987234})
	assert.Error(t, err)
}

func callConvertBinaryOp(expr *telemetryAPI.Expression, types FieldTypeMap, op binaryOp) (b *binaryExpr, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exprError); ok {
				err = e.error
			} else {
				panic(r)
			}
		}
	}()

	b = convertBinaryOp(expr, types, op).(*binaryExpr)
	return
}

func TestConvertBinaryOp(t *testing.T) {
	types := FieldTypeMap{
		"foo": ValueTypeSignedInt32,
	}

	expr := &telemetryAPI.Expression{Type: telemetryAPI.Expression_EQ}
	_, err := callConvertBinaryOp(expr, types, binaryOpEQ)
	assert.Error(t, err)

	expr = newBinaryExpr(telemetryAPI.Expression_EQ, nil, nil)
	_, err = callConvertBinaryOp(expr, types, binaryOpEQ)
	assert.Error(t, err)

	expr = newBinaryExpr(telemetryAPI.Expression_EQ, Identifier("foo"), nil)
	_, err = callConvertBinaryOp(expr, types, binaryOpEQ)
	assert.Error(t, err)

	lhs := Identifier("foo")
	rhs := Value(int32(8))
	ops := map[telemetryAPI.Expression_ExpressionType]binaryOp{
		telemetryAPI.Expression_LOGICAL_AND: binaryOpLogicalAnd,
		telemetryAPI.Expression_LOGICAL_OR:  binaryOpLogicalOr,
		telemetryAPI.Expression_EQ:          binaryOpEQ,
		telemetryAPI.Expression_NE:          binaryOpNE,
		telemetryAPI.Expression_LT:          binaryOpLT,
		telemetryAPI.Expression_LE:          binaryOpLE,
		telemetryAPI.Expression_GT:          binaryOpGT,
		telemetryAPI.Expression_GE:          binaryOpGE,
		telemetryAPI.Expression_BITWISE_AND: binaryOpBitwiseAnd,
	}
	for apiOp, op := range ops {
		testCase := telemetryAPI.Expression_ExpressionType_name[int32(apiOp)]
		expr = newBinaryExpr(apiOp, lhs, rhs)
		var b *binaryExpr
		b, err = callConvertBinaryOp(expr, types, op)
		if assert.NoError(t, err, testCase) {
			assert.Equal(t, op, b.op, testCase)
			assert.Equal(t, &identExpr{name: "foo", t: ValueTypeSignedInt32}, b.x, testCase)
			assert.Equal(t, &valueExpr{v: int32(8)}, b.y, testCase)
		}
	}
}

func callConvertUnaryOp(expr *telemetryAPI.Expression, types FieldTypeMap, op unaryOp) (u *unaryExpr, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exprError); ok {
				err = e.error
			} else {
				panic(r)
			}
		}
	}()

	u = convertUnaryOp(expr, types, op).(*unaryExpr)
	return
}

func TestConvertUnaryOp(t *testing.T) {
	types := FieldTypeMap{
		"foo": ValueTypeSignedInt32,
	}

	expr := &telemetryAPI.Expression{Type: telemetryAPI.Expression_IS_NULL}
	_, err := callConvertUnaryOp(expr, types, unaryOpIsNull)
	assert.Error(t, err)

	operand := Identifier("foo")
	ops := map[telemetryAPI.Expression_ExpressionType]unaryOp{
		telemetryAPI.Expression_IS_NULL:     unaryOpIsNull,
		telemetryAPI.Expression_IS_NOT_NULL: unaryOpIsNotNull,
	}
	for apiOp, op := range ops {
		testCase := telemetryAPI.Expression_ExpressionType_name[int32(apiOp)]
		expr = newUnaryExpr(apiOp, operand)
		var u *unaryExpr
		u, err = callConvertUnaryOp(expr, types, op)
		if assert.NoError(t, err, testCase) {
			assert.Equal(t, op, u.op, testCase)
			assert.Equal(t, &identExpr{name: "foo", t: ValueTypeSignedInt32}, u.x, testCase)
		}
	}
}

func TestConvertExpression(t *testing.T) {
	types := FieldTypeMap{
		"foo": ValueTypeUnsignedInt32,
	}

	e := &telemetryAPI.Expression{Type: telemetryAPI.Expression_IDENTIFIER}
	_, err := convertExpression(e, types)
	assert.Error(t, err)
	e = Identifier("foo$")
	_, err = convertExpression(e, types)
	assert.Error(t, err)
	e = Identifier("bar")
	_, err = convertExpression(e, types)
	assert.Error(t, err)

	e = &telemetryAPI.Expression{Type: telemetryAPI.Expression_VALUE}
	_, err = convertExpression(e, types)
	assert.Error(t, err)

	apiLHS := Identifier("foo")
	astLHS := &identExpr{name: "foo", t: ValueTypeUnsignedInt32}
	apiRHS := Value(uint32(8))
	astRHS := &valueExpr{v: uint32(8)}

	type testCase struct {
		apiExpr *telemetryAPI.Expression
		astExpr expr
	}
	testCases := []testCase{
		testCase{apiLHS, astLHS},
		testCase{apiRHS, astRHS},
		testCase{
			BitwiseAnd(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpBitwiseAnd},
		},
		testCase{
			Equal(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpEQ},
		},
		testCase{
			NotEqual(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpNE},
		},
		testCase{
			LessThan(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpLT},
		},
		testCase{
			LessThanEqualTo(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpLE},
		},
		testCase{
			GreaterThan(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpGT},
		},
		testCase{
			GreaterThanEqualTo(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpGE},
		},
		testCase{
			Like(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpLike},
		},
		testCase{
			IsNull(apiLHS),
			&unaryExpr{astLHS, unaryOpIsNull},
		},
		testCase{
			IsNotNull(apiLHS),
			&unaryExpr{astLHS, unaryOpIsNotNull},
		},
		testCase{
			LogicalAnd(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpLogicalAnd},
		},
		testCase{
			LogicalOr(apiLHS, apiRHS),
			&binaryExpr{astLHS, astRHS, binaryOpLogicalOr},
		},
	}
	for _, tc := range testCases {
		testCase := telemetryAPI.Expression_ExpressionType_name[int32(tc.apiExpr.Type)]
		var ast expr
		ast, err = convertExpression(tc.apiExpr, types)
		if assert.NoError(t, err, testCase) {
			assert.Equal(t, tc.astExpr, ast, testCase)
		}
	}
}
