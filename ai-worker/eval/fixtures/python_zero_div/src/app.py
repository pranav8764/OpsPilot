from fastapi import FastAPI, HTTPException

app = FastAPI()

@app.get("/calculate")
def calculate(numerator: float, denominator: float):
    # Bug: Division by zero is not handled, causing a 500 error instead of a clean 400.
    result = numerator / denominator
    return {"result": result}
