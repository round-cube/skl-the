from fastapi import FastAPI, Depends, HTTPException, status
from pydantic import BaseModel
from datetime import datetime
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
import os

app = FastAPI()

security = HTTPBearer()


TOKEN = os.getenv("TOKEN", "parking-meter")
STORAGE_FILE = os.getenv("STORAGE_FILE", "parkings.jsonl")


def verify_token(credentials: HTTPAuthorizationCredentials = Depends(security)):
    token = credentials.credentials
    if token != TOKEN:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid token value",
            headers={"WWW-Authenticate": "Bearer"},
        )
    return token


class CreateParkingRequest(BaseModel):
    vehicle_plate: str
    start_date_time: datetime
    end_date_time: datetime


class CreateParkingResponse(BaseModel):
    vehicle_plate: str
    start_date_time: datetime
    end_date_time: datetime
    total_seconds: int


@app.post("/parkings")
async def create_parking(parking: CreateParkingRequest, token: str = Depends(verify_token)) -> CreateParkingResponse:
    response = CreateParkingResponse(
        vehicle_plate=parking.vehicle_plate,
        start_date_time=parking.start_date_time,
        end_date_time=parking.end_date_time,
        total_seconds=(parking.end_date_time - parking.start_date_time).total_seconds(),
    )
    with open("parkings.jsonl", "a") as f:
        f.write(f"{response.model_dump_json()}\n")
    return response
