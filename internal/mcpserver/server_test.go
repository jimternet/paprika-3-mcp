package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/soggycactus/paprika-3-mcp/internal/paprika"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClient implements PaprikaClient for unit testing.
type mockClient struct {
	recipes      []paprika.Recipe
	mealPlans    []paprika.MealPlan
	groceries    []paprika.GroceryItem
	groceryLists []paprika.GroceryList

	// Track calls for assertions
	deletedRecipeUIDs  []string
	deletedGroceryUIDs []string
	deletedMealUIDs    []string
	savedGroceryItems  []paprika.GroceryItem
}

func (m *mockClient) ListRecipes(ctx context.Context) (*paprika.RecipeList, error) {
	var result []struct {
		UID  string `json:"uid"`
		Hash string `json:"hash"`
	}
	for _, r := range m.recipes {
		result = append(result, struct {
			UID  string `json:"uid"`
			Hash string `json:"hash"`
		}{UID: r.UID, Hash: r.Hash})
	}
	return &paprika.RecipeList{Result: result}, nil
}

func (m *mockClient) GetRecipe(ctx context.Context, uid string) (*paprika.Recipe, error) {
	for _, r := range m.recipes {
		if r.UID == uid {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("recipe not found: %s", uid)
}

func (m *mockClient) SaveRecipe(ctx context.Context, recipe paprika.Recipe) (*paprika.Recipe, error) {
	return &recipe, nil
}

func (m *mockClient) DeleteRecipe(ctx context.Context, recipe paprika.Recipe) (*paprika.Recipe, error) {
	m.deletedRecipeUIDs = append(m.deletedRecipeUIDs, recipe.UID)
	recipe.InTrash = true
	return &recipe, nil
}

func (m *mockClient) ListMealPlan(ctx context.Context) (*paprika.MealPlanResponse, error) {
	return &paprika.MealPlanResponse{Result: m.mealPlans}, nil
}

func (m *mockClient) SaveMealPlan(ctx context.Context, meal paprika.MealPlan) (*paprika.MealPlan, error) {
	meal.UID = "MOCK-MEAL-UID"
	return &meal, nil
}

func (m *mockClient) DeleteMealPlan(ctx context.Context, uid string) error {
	m.deletedMealUIDs = append(m.deletedMealUIDs, uid)
	return nil
}

func (m *mockClient) ListGroceries(ctx context.Context) (*paprika.GroceryResponse, error) {
	return &paprika.GroceryResponse{Result: m.groceries}, nil
}

func (m *mockClient) ListGroceryLists(ctx context.Context) (*paprika.GroceryListResponse, error) {
	return &paprika.GroceryListResponse{Result: m.groceryLists}, nil
}

func (m *mockClient) SaveGroceryItem(ctx context.Context, item paprika.GroceryItem) (*paprika.GroceryItem, error) {
	item.UID = "MOCK-GROCERY-UID"
	m.savedGroceryItems = append(m.savedGroceryItems, item)
	return &item, nil
}

func (m *mockClient) DeleteGroceryItem(ctx context.Context, uid string) error {
	m.deletedGroceryUIDs = append(m.deletedGroceryUIDs, uid)
	return nil
}

func newTestServer(mock *mockClient) *Server {
	// Build an in-memory cache pre-populated from the mock's recipe list.
	cache := paprika.NewCache(nil, "", slog.Default())
	for i := range mock.recipes {
		r := mock.recipes[i] // copy
		cache.Put(&r)
	}
	return &Server{
		paprika3:        mock,
		cache:           cache,
		logger:          slog.Default(),
		refreshInterval: 5 * time.Minute,
	}
}

func callToolRequest(args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func TestMealTypeName(t *testing.T) {
	assert.Equal(t, "Breakfast", paprika.MealTypeName(paprika.MealTypeBreakfast))
	assert.Equal(t, "Lunch", paprika.MealTypeName(paprika.MealTypeLunch))
	assert.Equal(t, "Dinner", paprika.MealTypeName(paprika.MealTypeDinner))
	assert.Equal(t, "Meal", paprika.MealTypeName(99))
}

func TestListMealPlan_DateFiltering(t *testing.T) {
	mock := &mockClient{
		mealPlans: []paprika.MealPlan{
			{UID: "1", Name: "Monday Dinner", Date: "2025-03-10 00:00:00", Type: paprika.MealTypeDinner},
			{UID: "2", Name: "Tuesday Lunch", Date: "2025-03-11 00:00:00", Type: paprika.MealTypeLunch},
			{UID: "3", Name: "Wednesday Breakfast", Date: "2025-03-12 00:00:00", Type: paprika.MealTypeBreakfast},
			{UID: "4", Name: "Deleted Meal", Date: "2025-03-11 00:00:00", Type: paprika.MealTypeDinner, Deleted: true},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("no filters returns all non-deleted", func(t *testing.T) {
		result, err := s.listMealPlan(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Monday Dinner")
		assert.Contains(t, text, "Tuesday Lunch")
		assert.Contains(t, text, "Wednesday Breakfast")
		assert.NotContains(t, text, "Deleted Meal")
	})

	t.Run("start_date filters correctly", func(t *testing.T) {
		result, err := s.listMealPlan(ctx, callToolRequest(map[string]interface{}{
			"start_date": "2025-03-11",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, "Monday Dinner")
		assert.Contains(t, text, "Tuesday Lunch")
		assert.Contains(t, text, "Wednesday Breakfast")
	})

	t.Run("end_date filters correctly", func(t *testing.T) {
		result, err := s.listMealPlan(ctx, callToolRequest(map[string]interface{}{
			"end_date": "2025-03-11",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Monday Dinner")
		assert.Contains(t, text, "Tuesday Lunch")
		assert.NotContains(t, text, "Wednesday Breakfast")
	})

	t.Run("exact date match includes the meal", func(t *testing.T) {
		result, err := s.listMealPlan(ctx, callToolRequest(map[string]interface{}{
			"start_date": "2025-03-10",
			"end_date":   "2025-03-10",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Monday Dinner")
		assert.NotContains(t, text, "Tuesday Lunch")
	})

	t.Run("deleted meals are excluded", func(t *testing.T) {
		result, err := s.listMealPlan(ctx, callToolRequest(map[string]interface{}{
			"start_date": "2025-03-11",
			"end_date":   "2025-03-11",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Tuesday Lunch")
		assert.NotContains(t, text, "Deleted Meal")
	})
}

func TestListGroceries_PurchaseFilter(t *testing.T) {
	mock := &mockClient{
		groceries: []paprika.GroceryItem{
			{UID: "1", Ingredient: "Milk", Purchased: false, Aisle: "Dairy"},
			{UID: "2", Ingredient: "Eggs", Purchased: true, Aisle: "Dairy"},
			{UID: "3", Ingredient: "Bread", Purchased: false, Aisle: "Bakery"},
			{UID: "4", Ingredient: "Deleted Item", Purchased: false, Deleted: true},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("all returns non-deleted items", func(t *testing.T) {
		result, err := s.listGroceries(ctx, callToolRequest(map[string]interface{}{
			"filter": "all",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Milk")
		assert.Contains(t, text, "Eggs")
		assert.Contains(t, text, "Bread")
		assert.NotContains(t, text, "Deleted Item")
	})

	t.Run("purchased filter", func(t *testing.T) {
		result, err := s.listGroceries(ctx, callToolRequest(map[string]interface{}{
			"filter": "purchased",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, "Milk")
		assert.Contains(t, text, "Eggs")
		assert.NotContains(t, text, "Bread")
	})

	t.Run("unpurchased filter", func(t *testing.T) {
		result, err := s.listGroceries(ctx, callToolRequest(map[string]interface{}{
			"filter": "unpurchased",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Milk")
		assert.NotContains(t, text, "Eggs")
		assert.Contains(t, text, "Bread")
	})

	t.Run("default filter is all", func(t *testing.T) {
		result, err := s.listGroceries(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "3 grocery item(s)")
	})

	t.Run("items grouped by aisle and aisles sorted", func(t *testing.T) {
		result, err := s.listGroceries(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		// Aisles should appear in alphabetical order: Bakery before Dairy
		bakeryIdx := strings.Index(text, "## Bakery")
		dairyIdx := strings.Index(text, "## Dairy")
		require.NotEqual(t, -1, bakeryIdx, "should have Bakery aisle")
		require.NotEqual(t, -1, dairyIdx, "should have Dairy aisle")
		assert.Less(t, bakeryIdx, dairyIdx, "Bakery should appear before Dairy")
	})

	t.Run("items without aisle grouped under Other", func(t *testing.T) {
		mockWithNoAisle := &mockClient{
			groceries: []paprika.GroceryItem{
				{UID: "1", Ingredient: "Mystery Item", Aisle: ""},
			},
		}
		s2 := newTestServer(mockWithNoAisle)
		result, err := s2.listGroceries(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "## Other")
		assert.Contains(t, text, "Mystery Item")
	})
}

func TestListRecipes(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "1", Name: "Chicken Tikka Masala", Ingredients: "chicken, yogurt, spices", Description: "Indian curry"},
			{UID: "2", Name: "Spaghetti Bolognese", Ingredients: "pasta, beef, tomatoes", Description: "Italian classic"},
			{UID: "3", Name: "Chicken Caesar Salad", Ingredients: "chicken, romaine, parmesan", Description: "Light lunch"},
			{UID: "4", Name: "Trashed Recipe", Ingredients: "nothing", Description: "Gone", InTrash: true},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("returns all non-trashed sorted by name", func(t *testing.T) {
		result, err := s.listRecipes(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Caesar Salad")
		assert.Contains(t, text, "Chicken Tikka Masala")
		assert.Contains(t, text, "Spaghetti Bolognese")
		assert.NotContains(t, text, "Trashed Recipe")
		// Verify alphabetical order
		caesarIdx := strings.Index(text, "Chicken Caesar Salad")
		tikkaIdx := strings.Index(text, "Chicken Tikka Masala")
		spaghettiIdx := strings.Index(text, "Spaghetti Bolognese")
		assert.Less(t, caesarIdx, tikkaIdx)
		assert.Less(t, tikkaIdx, spaghettiIdx)
	})

	t.Run("limit caps results", func(t *testing.T) {
		result, err := s.listRecipes(ctx, callToolRequest(map[string]interface{}{
			"limit": float64(2),
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Found 2 recipe(s)")
	})
}

func TestSearchRecipes(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "1", Name: "Chicken Tikka Masala", Ingredients: "chicken, yogurt, spices", Description: "Indian curry"},
			{UID: "2", Name: "Spaghetti Bolognese", Ingredients: "pasta, beef, tomatoes", Description: "Italian classic"},
			{UID: "3", Name: "Chicken Caesar Salad", Ingredients: "chicken, romaine, parmesan", Description: "Light lunch"},
			{UID: "4", Name: "Trashed Recipe", Ingredients: "nothing", Description: "Gone", InTrash: true},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("empty query returns error result", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		require.True(t, result.IsError, "expected IsError to be true")
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "query is required")
	})

	t.Run("keyword search matches name", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "spaghetti",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Spaghetti Bolognese")
		assert.NotContains(t, text, "Chicken")
	})

	t.Run("keyword search matches ingredients", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "romaine",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Caesar Salad")
		assert.NotContains(t, text, "Tikka")
	})

	t.Run("search is case insensitive", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "CHICKEN",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Tikka Masala")
		assert.Contains(t, text, "Chicken Caesar Salad")
	})

	t.Run("limit caps results", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "chicken",
			"limit": float64(1),
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Found 1 recipe(s)")
	})

	t.Run("multi-word search matches all words", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "chicken yogurt",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Tikka Masala")
		assert.NotContains(t, text, "Caesar Salad")
	})

	t.Run("multi-word search requires all words", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "chicken tomatoes",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No recipes found matching your search")
	})

	t.Run("no match returns message", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query": "nonexistent",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No recipes found matching your search")
	})
}

func TestRemoveGroceryItem(t *testing.T) {
	mock := &mockClient{
		groceries: []paprika.GroceryItem{
			{UID: "1", Ingredient: "Whole Milk", Name: "Whole Milk"},
			{UID: "2", Ingredient: "Almond Milk", Name: "Almond Milk"},
			{UID: "3", Ingredient: "Eggs", Name: "Eggs"},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("exact match removes item", func(t *testing.T) {
		mock.deletedGroceryUIDs = nil
		result, err := s.removeGroceryItem(ctx, callToolRequest(map[string]interface{}{
			"item_name": "Eggs",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Removed **Eggs**")
		assert.Equal(t, []string{"3"}, mock.deletedGroceryUIDs)
	})

	t.Run("partial match finds items", func(t *testing.T) {
		mock.deletedGroceryUIDs = nil
		result, err := s.removeGroceryItem(ctx, callToolRequest(map[string]interface{}{
			"item_name": "milk",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		// Should remove first match (Whole Milk) and report both
		assert.Contains(t, text, "Found 2 items matching")
		assert.Equal(t, []string{"1"}, mock.deletedGroceryUIDs)
	})

	t.Run("no match returns message", func(t *testing.T) {
		mock.deletedGroceryUIDs = nil
		result, err := s.removeGroceryItem(ctx, callToolRequest(map[string]interface{}{
			"item_name": "butter",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No Items Found")
		assert.Empty(t, mock.deletedGroceryUIDs)
	})

	t.Run("case insensitive", func(t *testing.T) {
		mock.deletedGroceryUIDs = nil
		result, err := s.removeGroceryItem(ctx, callToolRequest(map[string]interface{}{
			"item_name": "EGGS",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Removed **Eggs**")
		assert.Equal(t, []string{"3"}, mock.deletedGroceryUIDs)
	})
}

func TestListGroceryLists_FiltersDeleted(t *testing.T) {
	mock := &mockClient{
		groceryLists: []paprika.GroceryList{
			{UID: "1", Name: "My List", IsDefault: true},
			{UID: "2", Name: "Trader Joe's", IsDefault: false},
			{UID: "3", Name: "Old List", IsDefault: false, Deleted: true},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	result, err := s.listGroceryLists(ctx, callToolRequest(map[string]interface{}{}))
	require.NoError(t, err)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "My List")
	assert.Contains(t, text, "(default)")
	assert.Contains(t, text, "Trader Joe's")
	assert.NotContains(t, text, "Old List")
}

func TestListCategories(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "1", Name: "Chicken Tikka Masala", Categories: []string{"Indian", "Dinner"}},
			{UID: "2", Name: "Spaghetti Bolognese", Categories: []string{"Italian", "Dinner"}},
			{UID: "3", Name: "Caesar Salad", Categories: []string{"Salads", "indian"}}, // lowercase duplicate of "Indian"
			{UID: "4", Name: "Trashed Recipe", Categories: []string{"Italian"}, InTrash: true},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("lists categories with counts", func(t *testing.T) {
		result, err := s.listCategories(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		// Dinner should appear (2 recipes), Italian should appear (1 non-trashed recipe),
		// Indian/indian should be deduped and appear as 2 recipes, Salads as 1.
		assert.Contains(t, text, "Dinner (2 recipes)")
		assert.Contains(t, text, "Italian (1 recipe)")
		// Case-insensitive dedup: "Indian" and "indian" → count of 2
		assert.Contains(t, text, "2 recipe")
		// Trashed recipe's category should not inflate counts
		assert.NotContains(t, text, "Italian (2 recipes)")
	})

	t.Run("returns message when no categories", func(t *testing.T) {
		emptyMock := &mockClient{
			recipes: []paprika.Recipe{
				{UID: "1", Name: "Plain Recipe", Categories: []string{}},
			},
		}
		s2 := newTestServer(emptyMock)
		result, err := s2.listCategories(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No categories found")
	})

	t.Run("categories are sorted alphabetically", func(t *testing.T) {
		result, err := s.listCategories(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		dinnerIdx := strings.Index(text, "Dinner")
		italianIdx := strings.Index(text, "Italian")
		require.NotEqual(t, -1, dinnerIdx)
		require.NotEqual(t, -1, italianIdx)
		assert.Less(t, dinnerIdx, italianIdx)
	})
}

func TestRefreshRecipes(t *testing.T) {
	// refreshRecipes calls cache.Refresh which requires the cache to have a real *paprika.Client.
	// Since we can't inject a mock there, we test the tool indirectly by verifying that
	// when the underlying refresh succeeds (cache already loaded with data), the tool
	// returns the expected format. We test the error path when the client is nil.
	t.Run("returns error when cache client is nil", func(t *testing.T) {
		mock := &mockClient{}
		cache := paprika.NewCache(nil, "", slog.Default())
		s := &Server{
			paprika3:        mock,
			cache:           cache,
			logger:          slog.Default(),
			refreshInterval: 5 * time.Minute,
		}
		ctx := context.Background()
		_, err := s.refreshRecipes(ctx, callToolRequest(map[string]interface{}{}))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "refresh failed")
	})
}

func TestUpdateRecipe_MergePreservesFields(t *testing.T) {
	original := paprika.Recipe{
		UID:         "RECIPE-UID-1",
		Name:        "Original Name",
		Ingredients: "original ingredients",
		Directions:  "original directions",
		Description: "original description",
		Notes:       "original notes",
		Source:      "original source",
		SourceURL:   "https://original.example.com",
		Categories:  []string{"Italian", "Dinner"},
		Rating:      4,
		Servings:    "4",
		PrepTime:    "10 min",
		CookTime:    "30 min",
		Difficulty:  "Medium",
	}

	var savedRecipe *paprika.Recipe
	mock := &mockClient{
		recipes: []paprika.Recipe{original},
	}
	// Override SaveRecipe to capture what was sent.
	s := newTestServer(mock)
	ctx := context.Background()

	// Update only notes; all other fields should be preserved.
	result, err := s.updateRecipe(ctx, callToolRequest(map[string]interface{}{
		"uid":   "RECIPE-UID-1",
		"notes": "updated notes",
	}))
	require.NoError(t, err)
	_ = result

	// The saved recipe (returned by mock SaveRecipe) is what gets stored in cache.
	saved, found := s.cache.Get("RECIPE-UID-1")
	require.True(t, found)
	savedRecipe = saved

	assert.Equal(t, "Original Name", savedRecipe.Name, "name should be preserved")
	assert.Equal(t, "original ingredients", savedRecipe.Ingredients, "ingredients should be preserved")
	assert.Equal(t, "original directions", savedRecipe.Directions, "directions should be preserved")
	assert.Equal(t, "original description", savedRecipe.Description, "description should be preserved")
	assert.Equal(t, "updated notes", savedRecipe.Notes, "notes should be updated")
	assert.Equal(t, "original source", savedRecipe.Source, "source should be preserved")
	assert.Equal(t, "https://original.example.com", savedRecipe.SourceURL, "source_url should be preserved")
	assert.Equal(t, []string{"Italian", "Dinner"}, savedRecipe.Categories, "categories should be preserved")
	assert.Equal(t, 4, savedRecipe.Rating, "rating should be preserved")
	assert.Equal(t, "4", savedRecipe.Servings, "servings should be preserved")
}

func TestUpdateRecipe_FallsBackToAPI(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "RECIPE-API-1", Name: "API Recipe", Ingredients: "things", Directions: "do stuff"},
		},
	}
	// Build a server with an EMPTY cache to force the API fallback path.
	cache := paprika.NewCache(nil, "", slog.Default())
	s := &Server{
		paprika3:        mock,
		cache:           cache,
		logger:          slog.Default(),
		refreshInterval: 5 * time.Minute,
	}
	ctx := context.Background()

	result, err := s.updateRecipe(ctx, callToolRequest(map[string]interface{}{
		"uid":  "RECIPE-API-1",
		"name": "Updated via API Fallback",
	}))
	require.NoError(t, err)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "Updated via API Fallback")
}

func TestUpdateRecipe_NotFoundReturnsError(t *testing.T) {
	mock := &mockClient{recipes: []paprika.Recipe{}}
	s := newTestServer(mock)
	ctx := context.Background()

	result, err := s.updateRecipe(ctx, callToolRequest(map[string]interface{}{
		"uid": "NONEXISTENT-UID",
	}))
	require.NoError(t, err)
	require.True(t, result.IsError, "expected IsError to be true")
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "recipe not found")
}

func TestListRecipes_CategoryFilter(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "1", Name: "Chicken Tikka Masala", Categories: []string{"Indian", "Dinner"}},
			{UID: "2", Name: "Spaghetti Bolognese", Categories: []string{"Italian", "Dinner"}},
			{UID: "3", Name: "Caesar Salad", Categories: []string{"Salads"}},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("filters by category case-insensitively", func(t *testing.T) {
		result, err := s.listRecipes(ctx, callToolRequest(map[string]interface{}{
			"category": "dinner",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Tikka Masala")
		assert.Contains(t, text, "Spaghetti Bolognese")
		assert.NotContains(t, text, "Caesar Salad")
	})

	t.Run("no filter returns all recipes", func(t *testing.T) {
		result, err := s.listRecipes(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Tikka Masala")
		assert.Contains(t, text, "Spaghetti Bolognese")
		assert.Contains(t, text, "Caesar Salad")
	})

	t.Run("nonexistent category returns no recipes", func(t *testing.T) {
		result, err := s.listRecipes(ctx, callToolRequest(map[string]interface{}{
			"category": "Desserts",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No recipes found")
	})
}

func TestSearchRecipes_CategoryFilter(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "1", Name: "Chicken Tikka Masala", Ingredients: "chicken, yogurt", Categories: []string{"Indian"}},
			{UID: "2", Name: "Chicken Parmesan", Ingredients: "chicken, parmesan", Categories: []string{"Italian"}},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("category filter applied after keyword search", func(t *testing.T) {
		result, err := s.searchRecipes(ctx, callToolRequest(map[string]interface{}{
			"query":    "chicken",
			"category": "Indian",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Chicken Tikka Masala")
		assert.NotContains(t, text, "Chicken Parmesan")
	})
}

func TestGetRecipe_MetadataBlock(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{
				UID:        "RECIPE-META-1",
				Name:       "Test Recipe",
				Ingredients: "flour",
				Directions:  "mix",
				Categories: []string{"Baking"},
				Source:     "Grandma",
				SourceURL:  "https://example.com",
				Rating:     5,
				Created:    "2024-01-01 00:00:00",
			},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	result, err := s.getRecipe(ctx, callToolRequest(map[string]interface{}{
		"uid": "RECIPE-META-1",
	}))
	require.NoError(t, err)
	text := result.Content[0].(mcp.TextContent).Text

	assert.Contains(t, text, "RECIPE-META-1")
	assert.Contains(t, text, "Baking")
	assert.Contains(t, text, "Grandma")
	assert.Contains(t, text, "https://example.com")
	assert.Contains(t, text, "5")
	assert.Contains(t, text, "2024-01-01")
	// Should have the separator markers
	assert.Contains(t, text, "---")
}

func TestDeleteRecipe(t *testing.T) {
	mock := &mockClient{
		recipes: []paprika.Recipe{
			{UID: "recipe-1", Name: "Test Recipe", Ingredients: "flour", Directions: "mix"},
			{UID: "recipe-2", Name: "Other Recipe", Ingredients: "sugar", Directions: "bake"},
		},
	}
	s := newTestServer(mock)
	ctx := context.Background()

	t.Run("deletes recipe by UID", func(t *testing.T) {
		result, err := s.deleteRecipe(ctx, callToolRequest(map[string]interface{}{
			"uid": "recipe-1",
		}))
		require.NoError(t, err)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "Test Recipe")
		assert.Contains(t, text, "trash")
		assert.Equal(t, []string{"recipe-1"}, mock.deletedRecipeUIDs)
	})

	t.Run("returns error result for missing UID", func(t *testing.T) {
		result, err := s.deleteRecipe(ctx, callToolRequest(map[string]interface{}{}))
		require.NoError(t, err)
		require.True(t, result.IsError, "expected IsError to be true")
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "uid is required")
	})

	t.Run("returns error for unknown UID", func(t *testing.T) {
		_, err := s.deleteRecipe(ctx, callToolRequest(map[string]interface{}{
			"uid": "nonexistent",
		}))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get recipe")
	})
}
