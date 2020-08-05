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
	"errors"
	"fmt"
	"time"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
)

func convertValue(value *telemetryAPI.Value) (e expr) {
	switch value.GetType() {
	case telemetryAPI.ValueType_STRING:
		v, ok := value.GetValue().(*telemetryAPI.Value_StringValue)
		if !ok {
			exprRaise(errors.New("STRING value has no StringValue set"))
		}
		e = &valueExpr{v: v.StringValue}
	case telemetryAPI.ValueType_SINT8:
		v, ok := value.GetValue().(*telemetryAPI.Value_SignedValue)
		if !ok {
			exprRaise(errors.New("SINT8 value has no SignedValue set"))
		}
		e = &valueExpr{v: int8(v.SignedValue)}
	case telemetryAPI.ValueType_SINT16:
		v, ok := value.GetValue().(*telemetryAPI.Value_SignedValue)
		if !ok {
			exprRaise(errors.New("SINT16 value has no SignedValue set"))
		}
		e = &valueExpr{v: int16(v.SignedValue)}
	case telemetryAPI.ValueType_SINT32:
		v, ok := value.GetValue().(*telemetryAPI.Value_SignedValue)
		if !ok {
			exprRaise(errors.New("SINT32 value has no SignedValue set"))
		}
		e = &valueExpr{v: int32(v.SignedValue)}
	case telemetryAPI.ValueType_SINT64:
		v, ok := value.GetValue().(*telemetryAPI.Value_SignedValue)
		if !ok {
			exprRaise(errors.New("SINT64 value has no SignedValue set"))
		}
		e = &valueExpr{v: int64(v.SignedValue)}
	case telemetryAPI.ValueType_UINT8:
		v, ok := value.GetValue().(*telemetryAPI.Value_UnsignedValue)
		if !ok {
			exprRaise(errors.New("UINT8 value has no UnsignedValue set"))
		}
		e = &valueExpr{v: uint8(v.UnsignedValue)}
	case telemetryAPI.ValueType_UINT16:
		v, ok := value.GetValue().(*telemetryAPI.Value_UnsignedValue)
		if !ok {
			exprRaise(errors.New("UINT16 value has no UnsignedValue set"))
		}
		e = &valueExpr{v: uint16(v.UnsignedValue)}
	case telemetryAPI.ValueType_UINT32:
		v, ok := value.GetValue().(*telemetryAPI.Value_UnsignedValue)
		if !ok {
			exprRaise(errors.New("UINT32 value has no UnsignedValue set"))
		}
		e = &valueExpr{v: uint32(v.UnsignedValue)}
	case telemetryAPI.ValueType_UINT64:
		v, ok := value.GetValue().(*telemetryAPI.Value_UnsignedValue)
		if !ok {
			exprRaise(errors.New("UINT64 value has no UnsignedValue set"))
		}
		e = &valueExpr{v: uint64(v.UnsignedValue)}
	case telemetryAPI.ValueType_BOOL:
		v, ok := value.GetValue().(*telemetryAPI.Value_BoolValue)
		if !ok {
			exprRaise(errors.New("BOOL value has no BoolValue set"))
		}
		e = &valueExpr{v: v.BoolValue}
	case telemetryAPI.ValueType_DOUBLE:
		v, ok := value.GetValue().(*telemetryAPI.Value_DoubleValue)
		if !ok {
			exprRaise(errors.New("DOUBLE value has no DoubleValue set"))
		}
		e = &valueExpr{v: v.DoubleValue}
	case telemetryAPI.ValueType_TIMESTAMP:
		v, ok := value.GetValue().(*telemetryAPI.Value_TimestampValue)
		if !ok {
			exprRaise(errors.New("TIMESTAMP value has no TimestampValue set"))
		}
		t := v.TimestampValue
		e = &valueExpr{v: time.Unix(t.Seconds, int64(t.Nanos))}
	default:
		exprRaise(fmt.Errorf("Unrecognized value type %d", value.GetType()))
	}
	return
}

func convertBinaryOp(
	node *telemetryAPI.Expression,
	types FieldTypeMap,
	op binaryOp,
) expr {
	operands := node.GetBinaryOp()
	if operands == nil {
		exprRaise(errors.New("BinaryOp missing for binary operation node"))
	}
	if operands.Lhs == nil {
		exprRaise(errors.New("BinaryOp missing lhs"))
	}
	if operands.Rhs == nil {
		exprRaise(errors.New("BinaryOp missing rhs"))
	}

	logical := false
	switch node.GetType() {
	case telemetryAPI.Expression_LOGICAL_AND, telemetryAPI.Expression_LOGICAL_OR:
		logical = true
	}

	return &binaryExpr{
		op: op,
		x:  convertNode(operands.Lhs, types, logical),
		y:  convertNode(operands.Rhs, types, logical),
	}
}

func convertUnaryOp(
	node *telemetryAPI.Expression,
	types FieldTypeMap,
	op unaryOp,
) expr {
	operand := node.GetUnaryOp()
	if operand == nil {
		exprRaise(errors.New("UnaryOp missing for unary compare"))
	}

	return &unaryExpr{
		op: op,
		x:  convertNode(operand, types, false),
	}
}

func convertNode(
	node *telemetryAPI.Expression,
	types FieldTypeMap,
	logical bool,
) (r expr) {
	switch op := node.GetType(); op {
	case telemetryAPI.Expression_IDENTIFIER:
		ident := node.GetIdentifier()
		if ident == "" {
			exprRaise(errors.New("Identifier missing for IDENTIFIER node"))
		}
		if err := validateIdentifier(ident); err != nil {
			exprRaise(err)
		}
		if types == nil {
			r = &identExpr{name: ident}
		} else {
			if t, ok := types[ident]; ok {
				r = &identExpr{name: ident, t: t}
			} else {
				exprRaise(fmt.Errorf("Unknown IDENTIFIER %q",
					ident))
			}
		}
	case telemetryAPI.Expression_VALUE:
		value := node.GetValue()
		if value == nil {
			exprRaise(errors.New("Value missing for VALUE node"))
		}
		r = convertValue(value)
	case telemetryAPI.Expression_LOGICAL_AND:
		r = convertBinaryOp(node, types, binaryOpLogicalAnd)
	case telemetryAPI.Expression_LOGICAL_OR:
		r = convertBinaryOp(node, types, binaryOpLogicalOr)
	case telemetryAPI.Expression_EQ:
		r = convertBinaryOp(node, types, binaryOpEQ)
	case telemetryAPI.Expression_NE:
		r = convertBinaryOp(node, types, binaryOpNE)
	case telemetryAPI.Expression_LT:
		r = convertBinaryOp(node, types, binaryOpLT)
	case telemetryAPI.Expression_LE:
		r = convertBinaryOp(node, types, binaryOpLE)
	case telemetryAPI.Expression_GT:
		r = convertBinaryOp(node, types, binaryOpGT)
	case telemetryAPI.Expression_GE:
		r = convertBinaryOp(node, types, binaryOpGE)
	case telemetryAPI.Expression_LIKE:
		r = convertBinaryOp(node, types, binaryOpLike)
	case telemetryAPI.Expression_BITWISE_AND:
		r = convertBinaryOp(node, types, binaryOpBitwiseAnd)
	case telemetryAPI.Expression_IS_NULL:
		r = convertUnaryOp(node, types, unaryOpIsNull)
	case telemetryAPI.Expression_IS_NOT_NULL:
		r = convertUnaryOp(node, types, unaryOpIsNotNull)
	default:
		exprRaise(fmt.Errorf("Unrecognized expression type %d", node.GetType()))
	}
	return
}

func convertExpression(
	ae *telemetryAPI.Expression,
	types FieldTypeMap,
) (ast expr, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exprError); ok {
				err = e
			} else {
				panic(r)
			}
		}
	}()

	ast = convertNode(ae, types, true)
	return
}
