// Copyright 2017 Capsule8, Inc.
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
	"io"
	"io/ioutil"
	"reflect"
	"time"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"

	"github.com/golang/protobuf/ptypes/timestamp"
)

// Expression is a wrapper around expressions around the API. It may contain
// internal information that is used to better support the raw representation.
type Expression struct {
	ast   expr
	types FieldTypeMap
}

// ConvertExpression creates a new Expression from a telemetry API expression.
// If types is not nil, the types are bound to the expression during conversion.
// Otherwise types must be bound later before evaluation.
func ConvertExpression(
	tree *telemetryAPI.Expression,
	types FieldTypeMap,
) (*Expression, error) {
	ast, err := convertExpression(tree, types)
	if err != nil {
		return nil, err
	}
	if types != nil {
		// Types are already bound by conversion; just validate
		if _, err = validateTypes(ast); err != nil {
			return nil, err
		}
	}
	return &Expression{
		ast:   ast,
		types: types,
	}, nil
}

// ParseMode is a type representing a parser mode of operation
type ParseMode int

const (
	// ParseModeInvalid is an invalid parser mode.
	ParseModeInvalid ParseMode = iota

	// ParseModeKernelFilter instructs the parser to parse its input as a
	// Linux kernel tracing filter. The parser is a bit more strict in some
	// respects than the kernel's own parser. Specifically, it requires
	// that string literals are always enclosed in quotation marks. It is
	// also a bit more lax in some respects, although in some places where
	// it allows functionality that the kernel does not (e.g., quotation
	// marks in a string), kernel filter validation may fail.
	ParseModeKernelFilter
)

// ParseBytes parses an expression from a byte array. The input is always
// parsed as UTF-8 and does not allow embedded NUL runes.
func ParseBytes(
	input []byte,
	mode ParseMode,
	types FieldTypeMap,
) (*Expression, error) {
	switch mode {
	case ParseModeKernelFilter:
		// ok
	default:
		return nil, errors.New("invalid parse mode")
	}

	p := parser{
		input: scanner{
			input: input,
			mode:  mode,
		},
		mode:  mode,
		types: types,
	}
	return p.parse()
}

// Parse parses an expression from anything implementing the io.Reader
// interface. The input is always parsed as UTF-8 and does not allow embedded
// NUL runes.
func Parse(
	r io.Reader,
	mode ParseMode,
	types FieldTypeMap,
) (*Expression, error) {
	input, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytes(input, mode, types)
}

// ParseString parses an expression from a string. The input is always parsed
// as UTF-8 and does not allow embedded NUL runes.
func ParseString(
	s string,
	mode ParseMode,
	types FieldTypeMap,
) (*Expression, error) {
	return ParseBytes([]byte(s), mode, types)
}

// KernelFilterString returns a string representation of an expression that is
// suitable for setting a kernel perf_event filter. If the expression is not
// suitable for use as a kernel filter, the return will be the empty string.
func (expr *Expression) KernelFilterString() string {
	return expr.ast.KernelString()
}

// Return the string representation of an expression.
func (expr *Expression) String() string {
	return expr.ast.String()
}

// BindTypes binds field type information to an expression. Binding validates
// that the expression conforms to the type information being bound. Normally
// type information is bound during creation of the expression, but there are
// cases where that may not be possible, and so binding may be deferred to a
// later time. Type information must be bound before an expression can be
// evaluated.
func (expr *Expression) BindTypes(types FieldTypeMap) error {
	if expr.types != nil {
		return errors.New("type information is already bound")
	}
	if err := expr.ast.BindTypes(types); err != nil {
		return err
	}
	if _, err := validateTypes(expr.ast); err != nil {
		return err
	}
	expr.types = types
	return nil
}

// Evaluate evaluates an expression using the specified type and value
// information, and returns the result of that evaluation or an error. Any
// identifier not present in the types map is considered to be an undefined
// field and any reference to it is an error. Any identifier present in the
// types map, but not present in the values map is considered to be NULL; all
// comparisons against NULL will always evaluate FALSE.
func (expr *Expression) Evaluate(valueGetter FieldValueGetter) (interface{}, error) {
	if expr.types == nil {
		return nil, errors.New("types must be bound before evaluation")
	}
	return evaluateExpression(expr.ast, expr.types, valueGetter)
}

// ValidateKernelFilter determines whether an expression can be represented as
// a kernel filter string. If the result is nil, the kernel will most likely
// accept the expression as a filter. No check is done on the number of
// predicates in the expression, and some kernel versions do not support
// bitwise-and; however, this validator will accept bitwise-and because most
// do. Kernel limits on the number of predicates can vary, so it's not checked.
// If an expression passes this validation, it is not guaranteed that a given
// running kernel will absolutely accept it.
func (expr *Expression) ValidateKernelFilter() error {
	return validateKernelFilterTree(expr.ast)
}

// IsValueTrue determines whether a value's truth value is true or false.
// Strings are true if they contain one or more characters. Any numeric type
// is true if it is non-zero.
func IsValueTrue(i interface{}) bool {
	switch v := i.(type) {
	case string:
		return len(v) > 0
	case int8, int16, int32, int64:
		// can't use v here; would have to split out the cases
		return reflect.ValueOf(i).Int() != 0
	case uint8, uint16, uint32, uint64:
		// can't use v here; would have to split out the cases
		return reflect.ValueOf(i).Uint() != 0
	case bool:
		return v
	case float64:
		return v != 0.0
	case time.Time:
		return v.UnixNano() != 0
	}
	return false
}

// Support for internal error handling. For convert, evaluate, and validate
// functions that are all recursive, use panic/recover for propagating errors.
// This simplifies and neatens up the code by not having to manually propagate
// errors everywhere. It also helps with coverage in unit testing.

// Use a custom type for raising expression package errors so that any other
// panic gets propagated normally.
type exprError struct{ error }

func exprRaise(err error) {
	panic(exprError{err})
}

//////////////////////////////////////////////////////////////////////////////
//
//  Convenience APIs for building AST for protobuf API
//

// NewValue creates a new Value instance from a native Go type. If a Go type
// is used that does not have a Value equivalent, the return will be nil.
func NewValue(i interface{}) *telemetryAPI.Value {
	switch v := i.(type) {
	case string:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_STRING,
			Value: &telemetryAPI.Value_StringValue{StringValue: v},
		}
	case int8:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_SINT8,
			Value: &telemetryAPI.Value_SignedValue{SignedValue: int64(v)},
		}
	case int16:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_SINT16,
			Value: &telemetryAPI.Value_SignedValue{SignedValue: int64(v)},
		}
	case int32:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_SINT32,
			Value: &telemetryAPI.Value_SignedValue{SignedValue: int64(v)},
		}
	case int64:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_SINT64,
			Value: &telemetryAPI.Value_SignedValue{SignedValue: v},
		}
	case uint8:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_UINT8,
			Value: &telemetryAPI.Value_UnsignedValue{UnsignedValue: uint64(v)},
		}
	case uint16:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_UINT16,
			Value: &telemetryAPI.Value_UnsignedValue{UnsignedValue: uint64(v)},
		}
	case uint32:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_UINT32,
			Value: &telemetryAPI.Value_UnsignedValue{UnsignedValue: uint64(v)},
		}
	case uint64:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_UINT64,
			Value: &telemetryAPI.Value_UnsignedValue{UnsignedValue: v},
		}
	case bool:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_BOOL,
			Value: &telemetryAPI.Value_BoolValue{BoolValue: v},
		}
	case float64:
		return &telemetryAPI.Value{
			Type:  telemetryAPI.ValueType_DOUBLE,
			Value: &telemetryAPI.Value_DoubleValue{DoubleValue: v},
		}
	case *timestamp.Timestamp:
		return &telemetryAPI.Value{
			Type: telemetryAPI.ValueType_TIMESTAMP,
			Value: &telemetryAPI.Value_TimestampValue{
				TimestampValue: v,
			},
		}
	case time.Time:
		ts := &timestamp.Timestamp{
			Seconds: int64(v.UnixNano() / int64(time.Second)),
			Nanos:   int32(v.UnixNano() % int64(time.Second)),
		}
		return &telemetryAPI.Value{
			Type: telemetryAPI.ValueType_TIMESTAMP,
			Value: &telemetryAPI.Value_TimestampValue{
				TimestampValue: ts,
			},
		}
	}

	return nil
}

// Identifier creates a new IDENTIFIER Expression node.
func Identifier(name string) *telemetryAPI.Expression {
	return &telemetryAPI.Expression{
		Type: telemetryAPI.Expression_IDENTIFIER,
		Expr: &telemetryAPI.Expression_Identifier{
			Identifier: name,
		},
	}
}

// Value creates a new VALUE Expression node.
func Value(i interface{}) *telemetryAPI.Expression {
	return &telemetryAPI.Expression{
		Type: telemetryAPI.Expression_VALUE,
		Expr: &telemetryAPI.Expression_Value{Value: NewValue(i)},
	}
}

// IsNull creates a new IS_NULL unary Expression node
func IsNull(operand *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newUnaryExpr(telemetryAPI.Expression_IS_NULL, operand)
}

// IsNotNull creates a new IS_NOT_NULL unary Expression node
func IsNotNull(operand *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newUnaryExpr(telemetryAPI.Expression_IS_NOT_NULL, operand)
}

// LogicalAnd creates a new LOGICAL_AND binary Expression node. If either lhs
// or rhs is nil, the other will be returned
func LogicalAnd(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	if lhs == nil {
		return rhs
	}
	if rhs == nil {
		return lhs
	}
	return newBinaryExpr(telemetryAPI.Expression_LOGICAL_AND, lhs, rhs)
}

// LogicalOr creates a new LOGICAL_OR binary Expression node. If either lhs
// or rhs is nil, the other will be returned
func LogicalOr(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	if lhs == nil {
		return rhs
	}
	if rhs == nil {
		return lhs
	}
	return newBinaryExpr(telemetryAPI.Expression_LOGICAL_OR, lhs, rhs)
}

// BitwiseAnd creates a new BINARY_AND binary Expression node.
func BitwiseAnd(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_BITWISE_AND, lhs, rhs)
}

// Equal creates a new EQ binary Expression node.
func Equal(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_EQ, lhs, rhs)
}

// NotEqual creates a new NE binary Expression node.
func NotEqual(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_NE, lhs, rhs)
}

// LessThan creates a new LT binary Expression node.
func LessThan(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_LT, lhs, rhs)
}

// LessThanEqualTo creates a new LE binary Expression node.
func LessThanEqualTo(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_LE, lhs, rhs)
}

// GreaterThan creates a new GT binary expression node.
func GreaterThan(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_GT, lhs, rhs)
}

// GreaterThanEqualTo creates a new GE binary expression node.
func GreaterThanEqualTo(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_GE, lhs, rhs)
}

// Like creates a new LIKE binary Expression node.
func Like(lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return newBinaryExpr(telemetryAPI.Expression_LIKE, lhs, rhs)
}

func newBinaryExpr(op telemetryAPI.Expression_ExpressionType, lhs, rhs *telemetryAPI.Expression) *telemetryAPI.Expression {
	return &telemetryAPI.Expression{
		Type: op,
		Expr: &telemetryAPI.Expression_BinaryOp{
			BinaryOp: &telemetryAPI.BinaryOp{
				Lhs: lhs,
				Rhs: rhs,
			},
		},
	}
}

func newUnaryExpr(op telemetryAPI.Expression_ExpressionType, operand *telemetryAPI.Expression) *telemetryAPI.Expression {
	return &telemetryAPI.Expression{
		Type: op,
		Expr: &telemetryAPI.Expression_UnaryOp{
			UnaryOp: operand,
		},
	}
}
