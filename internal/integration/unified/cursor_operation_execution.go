// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package unified

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/internal/csot"
)

func executeIterateOnce(ctx context.Context, operation *operation) (*operationResult, error) {
	cursorEntity, err := entities(ctx).cursor(operation.Object)
	if err != nil {
		return nil, err
	}

	// If the cursor was created with a timeout, apply it to this iteration call.
	// This supports the CSOT spec requirement that timeoutMS on cursor constructors
	// should apply to each iteration call.
	if cursorEntity.timeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = csot.WithTimeout(ctx, cursorEntity.timeout)
		defer cancel()
	}

	// TryNext will attempt to get the next document, potentially issuing a single 'getMore'.
	if cursorEntity.cursor.TryNext(ctx) {
		// We don't expect the server to return malformed documents, so any errors from Decode here are treated
		// as fatal.
		var res bson.Raw
		if err := cursorEntity.cursor.Decode(&res); err != nil {
			return nil, fmt.Errorf("error decoding cursor result: %w", err)
		}

		return newDocumentResult(res, nil), nil
	}
	return newErrorResult(cursorEntity.cursor.Err()), nil
}

func executeIterateUntilDocumentOrError(ctx context.Context, operation *operation) (*operationResult, error) {
	cursorEntity, err := entities(ctx).cursor(operation.Object)
	if err != nil {
		return nil, err
	}

	// If the cursor was created with a timeout, apply it to this iteration call.
	// This supports the CSOT spec requirement that timeoutMS on cursor constructors
	// should apply to each iteration call.
	if cursorEntity.timeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = csot.WithTimeout(ctx, cursorEntity.timeout)
		defer cancel()
	}

	// Next will loop until there is either a result or an error.
	if cursorEntity.cursor.Next(ctx) {
		// We don't expect the server to return malformed documents, so any errors from Decode are treated as fatal.
		var res bson.Raw
		if err := cursorEntity.cursor.Decode(&res); err != nil {
			return nil, fmt.Errorf("error decoding cursor result: %w", err)
		}

		return newDocumentResult(res, nil), nil
	}
	return newErrorResult(cursorEntity.cursor.Err()), nil
}
